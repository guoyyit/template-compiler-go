package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestMissingInclude(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("page", `{% include "header" %}`)
	_, err := e.Compile("page")
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CompileError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "header") {
		t.Fatalf("error should mention missing template, got: %v", err)
	}
	if len(ce.Chain) != 2 || ce.Chain[0] != "page" || ce.Chain[1] != "header" {
		t.Fatalf("chain = %v, want [page header]", ce.Chain)
	}
}

func TestCircularReference(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("a", `{% include "b" %}`)
	e.AddTemplate("b", `{% include "c" %}`)
	e.AddTemplate("c", `{% include "a" %}`)
	_, err := e.Compile("a")
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CompileError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "circular reference") {
		t.Fatalf("expected circular reference error, got: %v", err)
	}
	want := []string{"a", "b", "c", "a"}
	if len(ce.Chain) != len(want) {
		t.Fatalf("chain = %v, want %v", ce.Chain, want)
	}
	for i := range want {
		if ce.Chain[i] != want[i] {
			t.Fatalf("chain = %v, want %v", ce.Chain, want)
		}
	}
}

func TestSelfReference(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("loop", `{% include "loop" %}`)
	_, err := e.Compile("loop")
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CompileError, got %v", err)
	}
	if len(ce.Chain) != 2 || ce.Chain[0] != "loop" || ce.Chain[1] != "loop" {
		t.Fatalf("chain = %v, want [loop loop]", ce.Chain)
	}
}

func TestDiamondReference(t *testing.T) {
	// 菱形依赖：a -> {b, c} -> d，不应误报循环
	e := NewEngine()
	e.AddTemplate("a", `{% include "b" %}{% include "c" %}`)
	e.AddTemplate("b", `{% include "d" %}`)
	e.AddTemplate("c", `{% include "d" %}`)
	e.AddTemplate("d", "leaf")
	if _, err := e.Compile("a"); err != nil {
		t.Fatalf("diamond reference should compile: %v", err)
	}
	out, err := e.Render("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "leafleaf" {
		t.Fatalf("output = %q, want %q", out, "leafleaf")
	}
}

func TestCompileErrorDoesNotPoisonCache(t *testing.T) {
	e := NewEngine()
	e.AddTemplate("a", `{% include "missing" %}`)
	if _, err := e.Compile("a"); err == nil {
		t.Fatal("expected compile error")
	}
	// 修复缺失引用后应能重新编译成功
	e.AddTemplate("missing", "found")
	if _, err := e.Compile("a"); err != nil {
		t.Fatalf("recompile after fix failed: %v", err)
	}
	out, err := e.Render("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "found" {
		t.Fatalf("output = %q", out)
	}
}
