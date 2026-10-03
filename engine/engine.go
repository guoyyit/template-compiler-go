package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Engine 是模板引擎门面，串联编译、缓存与渲染。
type Engine struct {
	store    *Store
	cache    *compileCache
	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	comp *CompiledTemplate
	err  error
}

func NewEngine() *Engine {
	return &Engine{
		store:    NewStore(),
		cache:    newCompileCache(),
		inflight: make(map[string]*call),
	}
}

// AddTemplate 注册模板源。
func (e *Engine) AddTemplate(name, text string) {
	e.store.Add(name, text)
}

// Compile 编译指定模板，递归校验引用图（缺失引用与循环引用
// 都会在编译阶段以可区分的错误终止），结果写入内容寻址缓存。
func (e *Engine) Compile(name string) (*CompiledTemplate, error) {
	return e.compileWith(name, nil)
}

func (e *Engine) compileWith(name string, chain []string) (*CompiledTemplate, error) {
	src, ok := e.store.Get(name)
	if !ok {
		return nil, &CompileError{File: name, Chain: append(append([]string{}, chain...), name), Msg: "template not found"}
	}
	for _, n := range chain {
		if n == name {
			return nil, &CompileError{File: name, Chain: append(append([]string{}, chain...), name), Msg: "circular reference"}
		}
	}

	key := hashText(src.Text)
	if comp, ok := e.cache.get(key); ok {
		return comp, nil
	}

	// 并发去重：同一内容的并发编译只执行一次。
	e.mu.Lock()
	if c, ok := e.inflight[key]; ok {
		e.mu.Unlock()
		<-c.done
		if c.err != nil {
			return nil, c.err
		}
		return c.comp, nil
	}
	c := &call{done: make(chan struct{})}
	e.inflight[key] = c
	e.mu.Unlock()

	comp, err := e.buildCompiled(name, src.Text, chain)

	e.mu.Lock()
	delete(e.inflight, key)
	e.mu.Unlock()
	c.comp = comp
	c.err = err
	close(c.done)

	if err != nil {
		return nil, err
	}
	e.cache.put(name, key, comp)
	return comp, nil
}

// buildCompiled 解析并编译模板，递归编译依赖。
func (e *Engine) buildCompiled(name, text string, chain []string) (*CompiledTemplate, error) {
	nodes, err := parseTemplate(text, name)
	if err != nil {
		return nil, err
	}
	comp, err := compileTemplate(name, nodes)
	if err != nil {
		return nil, err
	}
	nextChain := append(append([]string{}, chain...), name)
	for _, dep := range comp.Deps {
		if _, err := e.compileWith(dep, nextChain); err != nil {
			return nil, err
		}
	}
	return comp, nil
}

// Render 渲染模板并返回字符串。
func (e *Engine) Render(name string, data map[string]any) (string, error) {
	var sb strings.Builder
	if err := e.RenderTo(name, data, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RenderTo 渲染模板并写入 w。
func (e *Engine) RenderTo(name string, data map[string]any, w io.Writer) error {
	comp, err := e.Compile(name)
	if err != nil {
		return err
	}
	v := &vm{comp: comp, writer: w, engine: e}
	return v.execute(data)
}

// SaveArtifact 把已编译模板序列化写入 w（含版本与校验和）。
func (e *Engine) SaveArtifact(name string, w io.Writer) error {
	comp, err := e.Compile(name)
	if err != nil {
		return err
	}
	src, ok := e.store.Get(name)
	if !ok {
		return fmt.Errorf("template %q not registered", name)
	}
	a := &Artifact{
		FormatVersion: artifactVersion,
		Name:          name,
		SourceHash:    hashText(src.Text),
		Instr:         comp.Instr,
		Deps:          comp.Deps,
	}
	return encodeArtifact(w, a)
}

// LoadArtifact 读取产物并复用。产物损坏、版本不兼容或与当前源码
// 不匹配时返回错误，调用方应回退到重新编译。
func (e *Engine) LoadArtifact(r io.Reader) (string, error) {
	a, err := decodeArtifact(r)
	if err != nil {
		return "", err
	}
	if src, ok := e.store.Get(a.Name); ok {
		if hashText(src.Text) != a.SourceHash {
			return "", fmt.Errorf("artifact for %q does not match current source", a.Name)
		}
	}
	comp := &CompiledTemplate{Name: a.Name, Instr: a.Instr, Deps: a.Deps}
	e.cache.put(a.Name, a.SourceHash, comp)
	return a.Name, nil
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
