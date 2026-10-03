package template

import (
	"errors"
	"strings"
	"testing"
)

var escapeSrc = MapSource{
	"main": `{{ v }}|{{{ v }}}|{% if v %}{{ v }}{% endif %}{% include "part" with x=v %}`,
	"part": `[{{ x }}|{{{ x }}}]`,
}

var escapeData = map[string]any{"v": `<b>&"'"</b>`}

func TestMarshalRoundTripIdenticalOutput(t *testing.T) {
	tpl := mustCompile(t, "main", escapeSrc)
	blob, err := tpl.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	tpl2, err := Unmarshal(blob)
	if err != nil {
		t.Fatal(err)
	}
	out1, err := tpl.Render(escapeData)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := tpl2.Render(escapeData)
	if err != nil {
		t.Fatal(err)
	}
	if out1 != out2 {
		t.Fatalf("round-trip mismatch:\n%q\n%q", out1, out2)
	}
	// 转义行为在两条路径下完全一致
	if !strings.Contains(out2, "&lt;b&gt;") || !strings.Contains(out2, "|<b>&") {
		t.Fatalf("escaping changed after round-trip: %q", out2)
	}
}

func TestUnmarshalVersionMismatch(t *testing.T) {
	tpl := mustCompile(t, "main", escapeSrc)
	blob, _ := tpl.Marshal()
	// 篡改版本号
	bad := strings.Replace(string(blob), `"version":1`, `"version":999`, 1)
	_, err := Unmarshal([]byte(bad))
	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VersionError, got %v", err)
	}
	if ve.Got != 999 || ve.Want != FormatVersion {
		t.Fatalf("got=%d want=%d", ve.Got, ve.Want)
	}
}

func TestUnmarshalCorrupt(t *testing.T) {
	for _, blob := range [][]byte{
		[]byte("not json at all"),
		[]byte(`{"magic":"other","version":1}`),
		[]byte(`{"magic":"tplc","version":1,"name":"main","progs":{}}`),
		[]byte(`{"magic":"tplc","version":1,"name":"main","progs":{"main":{"body":[{"op":"output","exprIdx":7}],"exprs":[]}}}`),
	} {
		if _, err := Unmarshal(blob); err == nil {
			t.Fatalf("expected error for %q", blob)
		} else {
			var ve *VersionError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *VersionError for %q, got %v", blob, err)
			}
		}
	}
}

func TestLoadFallbackToRecompile(t *testing.T) {
	c := NewCache()
	// 损坏产物 -> 安全回退到重新编译，不崩溃
	tpl, err := c.Load("main", escapeSrc, []byte("garbage"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := tpl.Render(escapeData)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "&lt;b&gt;") {
		t.Fatalf("bad fallback output %q", out)
	}
	// 版本不兼容产物 -> 同样回退
	good := mustCompile(t, "main", escapeSrc)
	blob, _ := good.Marshal()
	stale := strings.Replace(string(blob), `"version":1`, `"version":42`, 1)
	tpl2, err := c.Load("main", escapeSrc, []byte(stale))
	if err != nil {
		t.Fatal(err)
	}
	out2, _ := tpl2.Render(escapeData)
	if out2 != out {
		t.Fatalf("fallback output mismatch %q vs %q", out2, out)
	}
}
