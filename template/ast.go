package template

// node 模板语法树节点。
type node interface{ nodePos() Pos }

type textNode struct {
	pos Pos
	val string
}

func (n *textNode) nodePos() Pos { return n.pos }

type outputNode struct {
	pos    Pos
	expr   expr
	escape bool
}

func (n *outputNode) nodePos() Pos { return n.pos }

type ifBranch struct {
	cond expr // nil 表示 else
	body []node
}

type ifNode struct {
	pos      Pos
	branches []ifBranch
}

func (n *ifNode) nodePos() Pos { return n.pos }

type forNode struct {
	pos    Pos
	keyVar string // 可空
	valVar string
	iter   expr
	body   []node
}

func (n *forNode) nodePos() Pos { return n.pos }

type includeNode struct {
	pos    Pos
	name   string
	params map[string]expr
}

func (n *includeNode) nodePos() Pos { return n.pos }
