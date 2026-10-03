package template

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// mutableSource 可并发读写的模板源，用于模拟源变化。
type mutableSource struct {
	mu sync.RWMutex
	m  map[string]string
}

func (s *mutableSource) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[name]
	return v, ok
}

func (s *mutableSource) set(name, content string) {
	s.mu.Lock()
	s.m[name] = content
	s.mu.Unlock()
}

func TestCacheHitAndMissIdentical(t *testing.T) {
	c := NewCache()
	// 未命中路径（编译）
	t1, err := c.Get("main", escapeSrc)
	if err != nil {
		t.Fatal(err)
	}
	// 命中路径（缓存复用）
	t2, err := c.Get("main", escapeSrc)
	if err != nil {
		t.Fatal(err)
	}
	if t1 != t2 {
		t.Fatal("cache hit should return the same compiled template")
	}
	out1, err := t1.Render(escapeData)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := t2.Render(escapeData)
	if err != nil {
		t.Fatal(err)
	}
	if out1 != out2 {
		t.Fatalf("hit/miss output mismatch: %q vs %q", out1, out2)
	}
	if !strings.Contains(out1, "&lt;b&gt;") {
		t.Fatalf("escaping wrong: %q", out1)
	}
}

func TestCacheInvalidatesOnSourceChange(t *testing.T) {
	src := &mutableSource{m: map[string]string{"main": "v1 {{ x }}"}}
	c := NewCache()
	t1, err := c.Get("main", src)
	if err != nil {
		t.Fatal(err)
	}
	out1, _ := t1.Render(map[string]any{"x": "!"})
	if out1 != "v1 !" {
		t.Fatalf("got %q", out1)
	}
	// 源变化后必须重新编译
	src.set("main", "v2 {{ x }}")
	t2, err := c.Get("main", src)
	if err != nil {
		t.Fatal(err)
	}
	if t1 == t2 {
		t.Fatal("expected recompile after source change")
	}
	out2, _ := t2.Render(map[string]any{"x": "!"})
	if out2 != "v2 !" {
		t.Fatalf("got %q", out2)
	}
	// 旧产物仍可用，不被破坏
	again, _ := t1.Render(map[string]any{"x": "?"})
	if again != "v1 ?" {
		t.Fatalf("old template corrupted: %q", again)
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := NewCache()
	t1, _ := c.Get("main", escapeSrc)
	c.Invalidate("main")
	t2, _ := c.Get("main", escapeSrc)
	if t1 == t2 {
		t.Fatal("expected recompile after Invalidate")
	}
}

func TestConcurrentRenderAndRecompile(t *testing.T) {
	src := &mutableSource{m: map[string]string{"main": `hello {{ name }} <{{ n }}>`}}
	c := NewCache()
	const workers = 8
	const iters = 200
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	// 并发渲染
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				tpl, err := c.Get("main", src)
				if err != nil {
					errs <- err
					return
				}
				out, err := tpl.Render(map[string]any{"name": "n", "n": i})
				if err != nil {
					errs <- err
					return
				}
				if !strings.HasPrefix(out, "hello n <") {
					errs <- fmt.Errorf("bad output %q", out)
					return
				}
			}
		}(w)
	}
	// 并发触发重新编译（源不断变化）
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				src.set("main", fmt.Sprintf("hello {{ name }} <{{ n }}> gen%d", i))
				if _, err := c.Get("main", src); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestConcurrentCompileSameName(t *testing.T) {
	// 多 goroutine 同时未命中同一模板：结果必须一致且无竞态
	c := NewCache()
	var wg sync.WaitGroup
	tpls := make([]*Template, 16)
	for i := range tpls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tpl, err := c.Get("main", escapeSrc)
			if err != nil {
				t.Error(err)
				return
			}
			tpls[i] = tpl
		}(i)
	}
	wg.Wait()
	for _, tpl := range tpls[1:] {
		if tpl != tpls[0] {
			t.Fatal("concurrent compiles returned different instances")
		}
	}
}
