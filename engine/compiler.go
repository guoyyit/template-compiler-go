package engine

// opcode 是字节码指令操作码。
type opcode int

const (
	opText opcode = iota
	opWrite      // 求值表达式并按 Safe 决定是否转义
	opJumpIfFalse
	opJump
	opForInit
	opForNext
	opInclude
)

// Instr 是一条字节码指令。Target 为跳转目标指令下标。
type Instr struct {
	Op     opcode
	Text   string
	Expr   *Expr
	Safe   bool
	Target int
	Var    string
	Name   string
	Params []IncludeParam
}

// CompiledTemplate 是编译产物（可序列化）。
type CompiledTemplate struct {
	Name  string
	Instr []Instr
	Deps  []string // 直接引用的模板名（去重、保序）
}

// compileTemplate 把 AST 编译为字节码。
func compileTemplate(name string, nodes []Node) (*CompiledTemplate, error) {
	c := &compiler{}
	if err := c.compileNodes(nodes); err != nil {
		return nil, err
	}
	return &CompiledTemplate{Name: name, Instr: c.code, Deps: c.deps}, nil
}

type compiler struct {
	code []Instr
	deps []string
	seen map[string]bool
}

func (c *compiler) emit(i Instr) int {
	c.code = append(c.code, i)
	return len(c.code) - 1
}

func (c *compiler) compileNodes(nodes []Node) error {
	for _, n := range nodes {
		switch node := n.(type) {
		case *TextNode:
			c.emit(Instr{Op: opText, Text: node.Text})
		case *VarNode:
			c.emit(Instr{Op: opWrite, Expr: &node.Expr, Safe: node.Safe})
		case *IfNode:
			jf := c.emit(Instr{Op: opJumpIfFalse, Expr: &node.Cond})
			if err := c.compileNodes(node.Body); err != nil {
				return err
			}
			jump := c.emit(Instr{Op: opJump})
			c.code[jf].Target = len(c.code)
			if err := c.compileNodes(node.Else); err != nil {
				return err
			}
			c.code[jump].Target = len(c.code)
		case *ForNode:
			c.emit(Instr{Op: opForInit, Var: node.Var, Expr: &node.Iterable})
			bodyStart := len(c.code)
			if err := c.compileNodes(node.Body); err != nil {
				return err
			}
			c.emit(Instr{Op: opForNext, Target: bodyStart})
			// 回填 forInit 的跳转目标（空列表时跳过循环体）
			c.code[bodyStart-1].Target = len(c.code)
		case *IncludeNode:
			c.emit(Instr{Op: opInclude, Name: node.Name, Params: node.Params})
			if c.seen == nil {
				c.seen = make(map[string]bool)
			}
			if !c.seen[node.Name] {
				c.seen[node.Name] = true
				c.deps = append(c.deps, node.Name)
			}
		}
	}
	return nil
}
