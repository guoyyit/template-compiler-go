package template

import "fmt"

// Pos 表示模板源码中的位置（1 起始的行列）。
type Pos struct {
	Line int
	Col  int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// SyntaxError 表示编译阶段发现的模板语法错误，可定位到行列。
type SyntaxError struct {
	Template string
	Pos      Pos
	Msg      string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("template %q: syntax error at %s: %s", e.Template, e.Pos, e.Msg)
}

// CycleError 表示模板引用图中发现循环引用，Path 为完整引用链。
type CycleError struct {
	Path []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cyclic template reference: %v", e.Path)
}

// MissingError 表示引用了不存在的模板，Path 为引用链。
type MissingError struct {
	Path    []string
	Missing string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("missing template %q referenced from %v", e.Missing, e.Path)
}

// RenderError 表示运行期渲染错误，携带模板标识与数据路径。
type RenderError struct {
	Template string
	Path     string
	Msg      string
}

func (e *RenderError) Error() string {
	return fmt.Sprintf("render %q failed at %q: %s", e.Template, e.Path, e.Msg)
}

// VersionError 表示反序列化产物时版本不兼容或数据损坏。
type VersionError struct {
	Got  int
	Want int
	Msg  string
}

func (e *VersionError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("incompatible compiled artifact: %s", e.Msg)
	}
	return fmt.Sprintf("incompatible compiled artifact: format version %d, want %d", e.Got, e.Want)
}
