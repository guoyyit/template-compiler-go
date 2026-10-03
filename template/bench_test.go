package template

import (
	"fmt"
	"strings"
	"testing"
)

// buildLargeSource 生成含 n 个循环项、每项含插值/条件/包含的大模板。
func buildLargeSource(items int) MapSource {
	var loop strings.Builder
	loop.WriteString("{% for i, it in items %}")
	loop.WriteString(`<div class="row">{{ it.name }}: {{ it.price * 2 }}</div>`)
	loop.WriteString(`{% if it.hot %}<b>{{ it.name }}</b>{% endif %}`)
	loop.WriteString(`{% include "cell" with v=it.name %}`)
	loop.WriteString("{% endfor %}")
	return MapSource{
		"main": `<html><body>` + loop.String() + `</body></html>`,
		"cell": `<span>{{ v }}</span>`,
	}
}

func benchData(n int) map[string]any {
	items := make([]any, n)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("item-%d", i), "price": i, "hot": i%3 == 0}
	}
	return map[string]any{"items": items}
}

// BenchmarkCompile 编译耗时基线（对照渲染，证明编译只发生一次）。
func BenchmarkCompile(b *testing.B) {
	src := buildLargeSource(100)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile("main", src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRender 不同规模模板的重复渲染。
// 运行方式: go test -bench=Render -benchmem
func BenchmarkRender(b *testing.B) {
	for _, size := range []int{1, 10, 100, 1000} {
		src := buildLargeSource(1) // 模板本身固定，数据规模变化
		tpl, err := Compile("main", src)
		if err != nil {
			b.Fatal(err)
		}
		data := benchData(size)
		b.Run(fmt.Sprintf("items=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := tpl.Render(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRenderBigTemplate 大模板（模板文本本身大）重复渲染。
func BenchmarkRenderBigTemplate(b *testing.B) {
	for _, blocks := range []int{10, 100, 1000} {
		src := buildLargeSource(blocks)
		tpl, err := Compile("main", src)
		if err != nil {
			b.Fatal(err)
		}
		data := benchData(blocks)
		b.Run(fmt.Sprintf("blocks=%d", blocks), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := tpl.Render(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRenderParallel 高并发渲染同一编译产物。
func BenchmarkRenderParallel(b *testing.B) {
	tpl, err := Compile("main", buildLargeSource(100))
	if err != nil {
		b.Fatal(err)
	}
	data := benchData(100)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := tpl.Render(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCacheGetHit 缓存命中路径开销。
func BenchmarkCacheGetHit(b *testing.B) {
	c := NewCache()
	src := buildLargeSource(10)
	if _, err := c.Get("main", src); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Get("main", src); err != nil {
			b.Fatal(err)
		}
	}
}
