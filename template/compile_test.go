package template

import (
	"errors"
	"strings"
	"testing"
)

func TestSyntaxErrorPosition(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantLine int
		wantCol  int
	}{
		{"unclosed output", "line1\nhello {{ name", 2, 7},
		{"unclosed stmt", "abc {% if x", 1, 5},
		{"if without endif", "{% if x %}text", 1, 3},
		{"for without endfor", "a\nb\n{% for x in ys %}", 3, 3},
		{"bad expr", "{{ a + }}", 1, 8},
		{"stray endif", "{% endif %}", 1, 3},
		{"unknown stmt", "{% wat %}", 1, 3},
		{"unterminated string", "{{ \"abc }}", 1, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile("t", MapSource{"t": tc.src})
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("expected *SyntaxError, got %v", err)
			}
			if se.Pos.Line != tc.wantLine || se.Pos.Col != tc.wantCol {
				t.Fatalf("pos = %v, want %d:%d (err=%v)", se.Pos, tc.wantLine, tc.wantCol, err)
			}
			if se.Template != "t" {
				t.Fatalf("template name = %q", se.Template)
			}
		})
	}
}

func TestCycleError(t *testing.T) {
	src := MapSource{
		"a": `{% include "b" %}`,
		"b": `{% include "c" %}`,
		"c": `{% include "a" %}`,
	}
	_, err := Compile("a", src)
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CycleError, got %v", err)
	}
	got := strings.Join(ce.Path, " -> ")
	if got != "a -> b -> c -> a" {
		t.Fatalf("cycle path = %q", got)
	}
}

func TestSelfCycle(t *testing.T) {
	_, err := Compile("a", MapSource{"a": `{% include "a" %}`})
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CycleError, got %v", err)
	}
}

func TestMissingError(t *testing.T) {
	src := MapSource{
		"a": `{% include "b" %}`,
		"b": `{% include "nope" %}`,
	}
	_, err := Compile("a", src)
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected *MissingError, got %v", err)
	}
	if me.Missing != "nope" {
		t.Fatalf("missing = %q", me.Missing)
	}
	if strings.Join(me.Path, " -> ") != "a -> b" {
		t.Fatalf("path = %v", me.Path)
	}
}

func TestErrorTypesDistinct(t *testing.T) {
	// 三类编译错误必须可区分
	_, synErr := Compile("t", MapSource{"t": "{{"})
	_, cycErr := Compile("a", MapSource{"a": `{% include "a" %}`})
	_, misErr := Compile("a", MapSource{"a": `{% include "x" %}`})
	var se *SyntaxError
	var ce *CycleError
	var me *MissingError
	if !errors.As(synErr, &se) || errors.As(synErr, &ce) || errors.As(synErr, &me) {
		t.Fatal("syntax error not distinct")
	}
	if !errors.As(cycErr, &ce) || errors.As(cycErr, &se) {
		t.Fatal("cycle error not distinct")
	}
	if !errors.As(misErr, &me) || errors.As(misErr, &se) || errors.As(misErr, &ce) {
		t.Fatal("missing error not distinct")
	}
}
