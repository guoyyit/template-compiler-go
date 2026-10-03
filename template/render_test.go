package template

import (
	"errors"
	"strings"
	"testing"
)

func mustCompile(t *testing.T, name string, src MapSource) *Template {
	t.Helper()
	tpl, err := Compile(name, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return tpl
}

func TestRenderBasics(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main": `Hello {{ user.name }}, you have {{ count + 1 }} messages.`,
	})
	out, err := tpl.Render(map[string]any{"user": map[string]any{"name": "Ann"}, "count": 2})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello Ann, you have 3 messages." {
		t.Fatalf("got %q", out)
	}
}

func TestRenderIfElifElse(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main": `{% if n > 10 %}big{% elif n > 5 %}mid{% else %}small{% endif %}`,
	})
	for n, want := range map[int64]string{20: "big", 7: "mid", 1: "small"} {
		out, err := tpl.Render(map[string]any{"n": n})
		if err != nil {
			t.Fatal(err)
		}
		if out != want {
			t.Fatalf("n=%d got %q want %q", n, out, want)
		}
	}
}

func TestRenderForWithIndex(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main": `{% for i, x in items %}{{ i }}={{ x }};{% endfor %}`,
	})
	out, err := tpl.Render(map[string]any{"items": []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "0=a;1=b;2=c;" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderExpressions(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main": `{{ a * 2 + 1 }} {{ not (a > 3) }} {{ a % 2 == 0 }} {{ s + "!" }}`,
	})
	out, err := tpl.Render(map[string]any{"a": 4, "s": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "9 false true hi!" {
		t.Fatalf("got %q", out)
	}
}

func TestEscapeDefaultAndRaw(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main": `{{ v }}|{{{ v }}}`,
	})
	out, err := tpl.Render(map[string]any{"v": `<a href="x">&`})
	if err != nil {
		t.Fatal(err)
	}
	want := `&lt;a href=&#34;x&#34;&gt;&amp;|<a href="x">&`
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestIncludeWithParams(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{
		"main":  `{% include "card" with title=user.name, n=1 + 1 %}`,
		"card":  `<h1>{{ title }}</h1><p>{{ n }}</p>`,
		"other": "unused",
	})
	out, err := tpl.Render(map[string]any{"user": map[string]any{"name": "T"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "<h1>T</h1><p>2</p>" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderErrorIsolation(t *testing.T) {
	src := MapSource{
		"bad":  `{{ a.b.c }}`,
		"good": `ok {{ x }}`,
	}
	bad := mustCompile(t, "bad", src)
	good := mustCompile(t, "good", src)

	_, err := bad.Render(map[string]any{"a": map[string]any{}})
	var re *RenderError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RenderError, got %v", err)
	}
	if re.Template != "bad" {
		t.Fatalf("template = %q", re.Template)
	}
	if !strings.Contains(re.Path, "a.b.c") {
		t.Fatalf("path = %q", re.Path)
	}
	// 单个模板渲染失败不影响其他模板
	out, err := good.Render(map[string]any{"x": 1})
	if err != nil || out != "ok 1" {
		t.Fatalf("good template polluted: out=%q err=%v", out, err)
	}
	// 失败模板用正确数据仍可渲染（无状态污染）
	out, err = bad.Render(map[string]any{"a": map[string]any{"b": map[string]any{"c": "fine"}}})
	if err != nil || out != "fine" {
		t.Fatalf("bad template not isolated: out=%q err=%v", out, err)
	}
}

func TestRenderUndefinedVar(t *testing.T) {
	tpl := mustCompile(t, "main", MapSource{"main": `{{ missing }}`})
	_, err := tpl.Render(map[string]any{})
	var re *RenderError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RenderError, got %v", err)
	}
	if re.Path != "missing" {
		t.Fatalf("path = %q", re.Path)
	}
}
