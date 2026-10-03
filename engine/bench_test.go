package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// 构造一个包含 N 个插值点的模板。
func sizedTemplate(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf("line %d: {{ value }} ({{ value }}/{{ total }})\n", i))
	}
	return sb.String()
}

func benchEngine(b *testing.B, tmpl string) *Engine {
	e := NewEngine()
	e.AddTemplate("t", tmpl)
	if _, err := e.Compile("t"); err != nil {
		b.Fatal(err)
	}
	return e
}

var benchData = map[string]any{"value": "hello", "total": 100}

// BenchmarkRenderSmall 小模板重复渲染（缓存命中）。
func BenchmarkRenderSmall(b *testing.B) {
	e := benchEngine(b, `Hello {{ name }}!`)
	data := map[string]any{"name": "world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Render("t", data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderLarge 大模板重复渲染（缓存命中）。
func BenchmarkRenderLarge(b *testing.B) {
	e := benchEngine(b, sizedTemplate(1000))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Render("t", benchData); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompile 冷编译开销（每次重新编译）。
func BenchmarkCompile(b *testing.B) {
	src := sizedTemplate(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := NewEngine()
		e.AddTemplate("t", src)
		if _, err := e.Compile("t"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderParallel 高并发渲染。
func BenchmarkRenderParallel(b *testing.B) {
	e := benchEngine(b, sizedTemplate(100))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := e.Render("t", benchData); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRenderParallelWithRecompile 并发渲染与并发重编译同时发生。
func BenchmarkRenderParallelWithRecompile(b *testing.B) {
	e := benchEngine(b, sizedTemplate(100))
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < b.N; j++ {
				e.AddTemplate("t", sizedTemplate(100))
			}
		}()
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := e.Render("t", benchData); err != nil {
				b.Fatal(err)
			}
		}
	})
	wg.Wait()
}

// BenchmarkRenderBySize 展示渲染开销与模板规模的关系。
func BenchmarkRenderBySize(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			e := benchEngine(b, sizedTemplate(n))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Render("t", benchData); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
