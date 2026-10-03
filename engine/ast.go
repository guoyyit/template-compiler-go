package engine

// Node 是所有模板语法树节点的接口。
type Node interface {
	nodePos() Pos
}

// TextNode 是一段静态文本。
type TextNode struct {
	Pos
	Text string
}

func (n *TextNode) nodePos() Pos { return n.Pos }

// VarNode 是变量插值：{{ expr }} 默认转义，{{{ expr }}} 原始输出。
type VarNode struct {
	Pos
	Expr Expr
	Safe bool
}

func (n *VarNode) nodePos() Pos { return n.Pos }

// IfNode 是条件分支。
type IfNode struct {
	Pos
	Cond Expr
	Body []Node
	Else []Node
}

func (n *IfNode) nodePos() Pos { return n.Pos }

// ForNode 是循环。
type ForNode struct {
	Pos
	Var      string
	Iterable Expr
	Body     []Node
}

func (n *ForNode) nodePos() Pos { return n.Pos }

// IncludeNode 是局部模板引用，可携带参数。
type IncludeNode struct {
	Pos
	Name   string
	Params []IncludeParam
}

func (n *IncludeNode) nodePos() Pos { return n.Pos }

// IncludeParam 是传递给被引用模板的具名参数。
type IncludeParam struct {
	Name  string
	Value Expr
}

// -------- 表达式（同时用于 AST 与可序列化 IR） --------

type ExprKind int

const (
	ExprLit ExprKind = iota
	ExprNil
	ExprPath
	ExprUnary
	ExprBinary
	ExprCall
)

// 字面量子类型（解决 false/0/"" 零值无法区分的问题）。
const (
	litStr = iota + 1
	litInt
	litFloat
	litBool
)

// PathSeg 是变量路径上的一段：Name 为字段名，或 Index 为下标表达式。
type PathSeg struct {
	Name  string
	Index *Expr
}

// Expr 是表达式树。全部使用具体类型字段（无接口），便于 gob 序列化。
type Expr struct {
	Kind  ExprKind
	Tag   int
	Pos   Pos
	Str   string
	Int   int64
	Float float64
	Bool  bool
	Path  []PathSeg
	Op    string
	Args  []Expr
	Func  string
}
