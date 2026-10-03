package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestEscaping(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ html }} {{{ raw }}}`)
	out, err := e.Render("t", map[string]any{
		"html": `<script>alert("x")</script>`,
		"raw":  `<b>bold</b>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; <b>bold</b>`
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestEscapingCacheConsistency(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ html }}|{{{ raw }}}`)
	data := map[string]any{"html": `<a>&</a>`, "raw": `<i>`}

	// 第一次渲染（缓存未命中路径）
	miss, err := e.Render("t", data)
	if err != nil {
		t.Fatal(err)
	}
	// 第二次渲染（缓存命中路径）
	hit, err := e.Render("t", data)
	if err != nil {
		t.Fatal(err)
	}
	if miss != hit {
		t.Fatalf("cache miss %q != cache hit %q", miss, hit)
	}
	if !strings.Contains(miss, "&lt;a&gt;&amp;&lt;/a&gt;") {
		t.Fatalf("escaping wrong: %q", miss)
	}
}

func TestExpressions(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ a + b * 2 }}|{{ s == "x" }}|{{ len(items) }}|{{ items[1] }}|{{ flag && false }}|{{ !flag }}`)
	out, err := e.Render("t", map[string]any{
		"a": 1, "b": 2, "s": "x", "flag": true, "items": []string{"p", "q", "r"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "5|true|3|q|false|false"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestPathIndexError(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ user.address.city }}`)
	_, err := e.Render("t", map[string]any{"user": map[string]any{"address": map[string]any{}}})
	var re *RenderError
	if !errors.As(err, &re) {
		t.Fatalf("expected RenderError, got %T: %v", err, err)
	}
	if re.Template != "t" {
		t.Fatalf("template = %q, want t", re.Template)
	}
	if re.Path != "user.address.city" {
		t.Fatalf("path = %q, want user.address.city", re.Path)
	}
}

func TestRenderErrorIsolation(t *testing.T) {
	e := NewEngine()
	// 坏模板：渲染时会在 include 内部失败
	e.AddTemplate("bad", `{% include "inner" %}`)
	e.AddTemplate("inner", `{{ 1 / 0 }}`)
	// 好模板：不受坏模板影响
	e.AddTemplate("good", `OK`)

	if _, err := e.Render("bad", nil); err == nil {
		t.Fatal("expected render error")
	}
	// 缓存未被污染，好模板照常渲染
	out, err := e.Render("good", nil)
	if err != nil {
		t.Fatalf("good template failed after bad render: %v", err)
	}
	if out != "OK" {
		t.Fatalf("output = %q", out)
	}
	// 坏模板修复后可重新渲染
	e.AddTemplate("inner", `fixed`)
	out, err = e.Render("bad", nil)
	if err != nil {
		t.Fatalf("bad template failed after fix: %v", err)
	}
	if out != "fixed" {
		t.Fatalf("output = %q", out)
	}
}

func TestNilAndEmpty(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `[{{ missing }}]`)
	out, err := e.Render("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "[]" {
		t.Fatalf("output = %q, want []", out)
	}
}

func TestNoParamIncludePassesContext(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("page", `{% include "partial" %}`)
	e.AddTemplate("partial", `{{ title }}`)
	out, err := e.Render("page", map[string]any{"title": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Fatalf("output = %q", out)
	}
}

func TestNestedBlocks(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{% for g in groups %}<{{ g.name }}>{% for u in g.users %}{% if u.active %}+{{ u.name }}{% else %}-{{ u.name }}{% end %}{% end %}{% end %}`)
	out, err := e.Render("t", map[string]any{
		"groups": []map[string]any{
			{"name": "g1", "users": []map[string]any{{"name": "a", "active": true}, {"name": "b", "active": false}}},
			{"name": "g2", "users": []map[string]any{{"name": "c", "active": true}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "<g1>+a-b<g2>+c"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestIncludeInLoop(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("page", `{% for u in users %}{% include "card" name=u.name %}{% end %}`)
	e.AddTemplate("card", `[{{ name }}]`)
	out, err := e.Render("page", map[string]any{
		"users": []map[string]any{{"name": "a"}, {"name": "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "[a][b]" {
		t.Fatalf("output = %q", out)
	}
}

func TestMapLenAndIndex(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ len(m) }}|{{ m["k"] }}`)
	out, err := e.Render("t", map[string]any{
		"m": map[string]any{"k": "v", "x": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "2|v" {
		t.Fatalf("output = %q, want 2|v", out)
	}
}

func TestStringConcat(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ "a" + "b" }}`)
	out, err := e.Render("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ab" {
		t.Fatalf("output = %q", out)
	}
}
