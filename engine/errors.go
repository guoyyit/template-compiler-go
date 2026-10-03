package engine

import "fmt"

// Pos 表示源码中的位置（行、列均从 1 开始）。
type Pos struct {
	Line int
	Col  int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// SyntaxError 表示模板源码中的语法错误，位置可定位到行列。
type SyntaxError struct {
	File string
	Pos  Pos
	Msg  string
}

func (e *SyntaxError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: syntax error: %s", e.File, e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return fmt.Sprintf("%d:%d: syntax error: %s", e.Pos.Line, e.Pos.Col, e.Msg)
}

// CompileError 表示编译阶段错误（引用缺失、循环引用等），
// Chain 保存完整的引用链路，例如 ["page", "layout", "header"]。
type CompileError struct {
	File  string
	Chain []string
	Msg   string
}

func (e *CompileError) Error() string {
	chain := ""
	for i, n := range e.Chain {
		if i > 0 {
			chain += " -> "
		}
		chain += n
	}
	if chain != "" {
		return fmt.Sprintf("compile error in %s (chain %s): %s", e.File, chain, e.Msg)
	}
	return fmt.Sprintf("compile error in %s: %s", e.File, e.Msg)
}

// RenderError 表示渲染期错误，携带模板标识与数据路径。
type RenderError struct {
	Template string
	Path     string
	Msg      string
	Cause    error
}

func (e *RenderError) Error() string {
	s := fmt.Sprintf("render error in template %q", e.Template)
	if e.Path != "" {
		s += fmt.Sprintf(" at %s", e.Path)
	}
	s += ": " + e.Msg
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	return s
}

func (e *RenderError) Unwrap() error { return e.Cause }

// 产物相关错误。
var (
	ErrArtifactIncompatible = fmt.Errorf("artifact format version incompatible")
	ErrArtifactCorrupt      = fmt.Errorf("artifact corrupt")
)
