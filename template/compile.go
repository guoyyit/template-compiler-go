package template

// Source 提供按名字取模板源码的能力。
type Source interface {
	Get(name string) (string, bool)
}

// MapSource 是基于 map 的 Source 实现。
type MapSource map[string]string

// Get 实现 Source。
func (m MapSource) Get(name string) (string, bool) {
	s, ok := m[name]
	return s, ok
}

// Compile 编译入口模板及其全部引用。
// 语法错误、循环引用、引用缺失均在此阶段以可区分的错误返回。
func Compile(name string, src Source) (*Template, error) {
	c := &compiler{src: src, progs: map[string]*program{}}
	if err := c.compileAll(name, nil); err != nil {
		return nil, err
	}
	return &Template{name: name, progs: c.progs}, nil
}

type compiler struct {
	src   Source
	progs map[string]*program
}

// compileAll 以 DFS 编译 name 及其依赖，path 为当前引用链（用于环检测与错误信息）。
func (c *compiler) compileAll(name string, path []string) error {
	for i, p := range path {
		if p == name {
			return &CycleError{Path: append(append([]string{}, path[i:]...), name)}
		}
	}
	if _, ok := c.progs[name]; ok {
		return nil // 已编译（菱形依赖只编译一次）
	}
	raw, ok := c.src.Get(name)
	if !ok {
		return &MissingError{Path: append([]string{}, path...), Missing: name}
	}
	toks, err := lex(raw)
	if err != nil {
		if se, ok := err.(*SyntaxError); ok {
			se.Template = name
		}
		return err
	}
	nodes, err := parse(toks)
	if err != nil {
		if se, ok := err.(*SyntaxError); ok {
			se.Template = name
		}
		return err
	}
	prog := &program{name: name}
	prog.body = c.compileNodes(nodes, prog)
	c.progs[name] = prog
	// 递归编译依赖
	path = append(append([]string{}, path...), name)
	for _, dep := range collectIncludes(nodes) {
		if err := c.compileAll(dep, path); err != nil {
			return err
		}
	}
	return nil
}

// collectIncludes 收集语法树中直接引用的模板名。
func collectIncludes(nodes []node) []string {
	var out []string
	var walk func(ns []node)
	walk = func(ns []node) {
		for _, n := range ns {
			switch t := n.(type) {
			case *includeNode:
				out = append(out, t.name)
			case *ifNode:
				for _, b := range t.branches {
					walk(b.body)
				}
			case *forNode:
				walk(t.body)
			}
		}
	}
	walk(nodes)
	return out
}

// addExpr 将表达式加入程序的表达式表，返回下标。
func (c *compiler) addExpr(p *program, e expr) int {
	p.exprs = append(p.exprs, e)
	return len(p.exprs) - 1
}

func (c *compiler) compileNodes(nodes []node, p *program) []instr {
	var out []instr
	for _, n := range nodes {
		switch t := n.(type) {
		case *textNode:
			if t.val != "" {
				out = append(out, instr{op: opText, text: t.val})
			}
		case *outputNode:
			out = append(out, instr{op: opOutput, exprIdx: c.addExpr(p, t.expr), escape: t.escape})
		case *ifNode:
			ins := instr{op: opIf}
			for _, b := range t.branches {
				br := branch{condIdx: -1}
				if b.cond != nil {
					br.condIdx = c.addExpr(p, b.cond)
				}
				br.body = c.compileNodes(b.body, p)
				ins.branches = append(ins.branches, br)
			}
			out = append(out, ins)
		case *forNode:
			out = append(out, instr{
				op:      opFor,
				keyVar:  t.keyVar,
				valVar:  t.valVar,
				iterIdx: c.addExpr(p, t.iter),
				body:    c.compileNodes(t.body, p),
			})
		case *includeNode:
			ins := instr{op: opCall, callName: t.name, callParams: map[string]int{}}
			for k, v := range t.params {
				ins.callParams[k] = c.addExpr(p, v)
			}
			out = append(out, ins)
		}
	}
	return out
}
