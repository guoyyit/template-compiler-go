package template

// opCode 编译产物指令码。
type opCode int

const (
	opText opCode = iota
	opOutput
	opIf
	opFor
	opCall
)

// branch 条件分支：condIdx 为表达式下标，-1 表示 else。
type branch struct {
	condIdx int
	body    []instr
}

// instr 编译指令（树形结构，循环与分支内嵌 body）。
type instr struct {
	op      opCode
	text    string // opText 的文本
	exprIdx int    // opOutput 的表达式下标
	escape  bool   // opOutput 是否转义

	branches []branch // opIf

	keyVar  string // opFor，可空
	valVar  string
	iterIdx int
	body    []instr

	callName   string         // opCall 被引用模板名
	callParams map[string]int // 参数名 -> 表达式下标
}

// program 是单个模板的编译产物。
type program struct {
	name  string
	body  []instr
	exprs []expr
}

// Template 是一组可渲染的编译产物：入口模板加全部依赖。
type Template struct {
	name  string
	progs map[string]*program
}

// Name 返回入口模板名。
func (t *Template) Name() string { return t.name }
