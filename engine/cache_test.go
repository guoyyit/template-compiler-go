package engine

import (
	"sync"
	"testing"
)

func TestCacheInvalidation(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `v1`)
	out1, err := e.Render("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out1 != "v1" {
		t.Fatalf("out1 = %q", out1)
	}
	// 源模板变化后必须重新编译
	e.AddTemplate("t", `v2`)
	out2, err := e.Render("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out2 != "v2" {
		t.Fatalf("out2 = %q, want v2 (cache not invalidated)", out2)
	}
}

func TestCacheContentAddressing(t *testing.T) {
	e := NewEngine()
	// 相同内容的不同模板共享编译产物
	e.AddTemplate("a", `same`)
	e.AddTemplate("b", `same`)
	ca, err := e.Compile("a")
	if err != nil {
		t.Fatal(err)
	}
	cb, err := e.Compile("b")
	if err != nil {
		t.Fatal(err)
	}
	if ca != cb {
		t.Fatal("identical content should share the same compiled artifact")
	}
}

func TestConcurrentRenderAndRecompile(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ n }}`)
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	// 并发渲染
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := e.Render("t", map[string]any{"n": j}); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}
	// 并发重新编译（源变化）
	for k := 0; k < 5; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				e.AddTemplate("t", `{{ n }}`)
				if _, err := e.Compile("t"); err != nil {
					errCh <- err
					return
				}
			}
		}(k)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestConcurrentCompileSameKey(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ n }}`)
	var wg sync.WaitGroup
	errCh := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.Compile("t"); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent compile error: %v", err)
	}
}
