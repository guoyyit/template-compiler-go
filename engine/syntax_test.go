package engine

import (
	"errors"
	"testing"
)

func TestSyntaxErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
		col  int
	}{
		{"unterminated interpolation", "Hello {{ name", 1, 7},
		{"unterminated tag", "{% if true %}", 1, 4},
		{"unknown tag", "{% frob %}", 1, 4},
		{"missing end for", "{% for x in y %}hi", 1, 4},
		{"missing end if", "{% if true %}hi", 1, 4},
		{"bad expression", "{{ 1 + }}", 1, 8},
		{"unterminated string", `{{ "abc }}`, 1, 4},
		{"unterminated comment", "hi {# comment", 1, 4},
		{"if without condition", "{% if %}x{% end %}", 1, 7},
		{"for without in", "{% for x items %}x{% end %}", 1, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTemplate(tc.src, "t")
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("expected SyntaxError, got %T: %v", err, err)
			}
			if se.Pos.Line != tc.line || se.Pos.Col != tc.col {
				t.Fatalf("position = %d:%d, want %d:%d (msg: %s)", se.Pos.Line, se.Pos.Col, tc.line, tc.col, se.Msg)
			}
		})
	}
}

func TestSyntaxErrorMultilinePosition(t *testing.T) {
	src := "line one\nline two\n{{ broken"
	_, err := parseTemplate(src, "t")
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("expected SyntaxError, got %v", err)
	}
	if se.Pos.Line != 3 || se.Pos.Col != 1 {
		t.Fatalf("position = %d:%d, want 3:1", se.Pos.Line, se.Pos.Col)
	}
}

func TestValidSyntax(t *testing.T) {
	srcs := []string{
		"plain text",
		"{{ a }}",
		"{{{ a }}}",
		"{% if a %}yes{% else %}no{% end %}",
		"{% for x in items %}{{ x }}{% end %}",
		`{% include "partial" x=1 y=2 %}`,
		"{{ a.b[0].c }}",
		"{{ (a + b) * c == d && !e || f }}",
		"{{ len(items) }}",
		"{{ a[0] }}",
		"{% if a > 1 and b < 2 %}x{% end %}",
		"{# comment #}text",
	}
	for _, src := range srcs {
		if _, err := parseTemplate(src, "t"); err != nil {
			t.Errorf("expected valid, got error: %v (src=%q)", err, src)
		}
	}
}
