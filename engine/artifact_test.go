package engine

import (
	"bytes"
	"errors"
	"testing"
)

func TestArtifactRoundTrip(t *testing.T) {	e := NewEngine()
	e.AddTemplate("page", `{% for item in items %}[{{ item }}]{% end %}`)
	var buf bytes.Buffer
	if err := e.SaveArtifact("page", &buf); err != nil {
		t.Fatal(err)
	}

	e2 := NewEngine()
	e2.AddTemplate("page", `{% for item in items %}[{{ item }}]{% end %}`)
	name, err := e2.LoadArtifact(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if name != "page" {
		t.Fatalf("name = %q", name)
	}
	out, err := e2.Render("page", map[string]any{"items": []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "[a][b]" {
		t.Fatalf("output = %q", out)
	}
}

func TestArtifactCorruptFallback(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ x }}`)
	var buf bytes.Buffer
	if err := e.SaveArtifact("t", &buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// 损坏 payload（最后一个字节翻转）
	corrupt := make([]byte, len(data))
	copy(corrupt, data)
	corrupt[len(corrupt)-1] ^= 0xFF

	e2 := NewEngine()
	e2.AddTemplate("t", `{{ x }}`)
	if _, err := e2.LoadArtifact(bytes.NewReader(corrupt)); !errors.Is(err, ErrArtifactCorrupt) {
		t.Fatalf("expected ErrArtifactCorrupt, got %v", err)
	}
	// 回退后重新编译渲染正常
	out, err := e2.Render("t", map[string]any{"x": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestArtifactIncompatibleFallback(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ x }}`)
	var buf bytes.Buffer
	if err := e.SaveArtifact("t", &buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// 篡改版本号
	data[4] = 99

	e2 := NewEngine()
	e2.AddTemplate("t", `{{ x }}`)
	if _, err := e2.LoadArtifact(bytes.NewReader(data)); !errors.Is(err, ErrArtifactIncompatible) {
		t.Fatalf("expected ErrArtifactIncompatible, got %v", err)
	}
	out, err := e2.Render("t", map[string]any{"x": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestArtifactSourceMismatch(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `v1`)
	var buf bytes.Buffer
	if err := e.SaveArtifact("t", &buf); err != nil {
		t.Fatal(err)
	}
	// 源码已变化，产物不应被复用
	e2 := NewEngine()
	e2.AddTemplate("t", `v2`)
	if _, err := e2.LoadArtifact(&buf); err == nil {
		t.Fatal("expected source mismatch error")
	}
	out, err := e2.Render("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "v2" {
		t.Fatalf("output = %q, want v2", out)
	}
}

func TestArtifactGarbage(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("t", `{{ x }}`)
	if _, err := e.LoadArtifact(bytes.NewReader([]byte("garbage data"))); err == nil {
		t.Fatal("expected error on garbage input")
	}
	// 引擎仍可正常编译渲染
	out, err := e.Render("t", map[string]any{"x": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestArtifactWithInclude(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("page", `{% include "header" %} body`)
	e.AddTemplate("header", `HEAD`)
	var buf bytes.Buffer
	if err := e.SaveArtifact("page", &buf); err != nil {
		t.Fatal(err)
	}
	e2 := NewEngine()
	e2.AddTemplate("page", `{% include "header" %} body`)
	e2.AddTemplate("header", `HEAD`)
	if _, err := e2.LoadArtifact(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := e2.Render("page", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "HEAD body" {
		t.Fatalf("output = %q", out)
	}
}
