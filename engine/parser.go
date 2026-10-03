package engine

import (
	"fmt"
	"strconv"
)

// parser 把 token 流解析为 AST。
type parser struct {
	lx  *lexer
	cur token
}

func parseTemplate(src, file string) ([]Node, error) {
	p := &parser{lx: newLexer(src, file)}
	if err := p.nextText(); err != nil {
		return nil, err
	}
	nodes, _, err := p.parseBlock(map[string]bool{"end": true, "else": true})
	if err != nil {
		return nil, err
	}
	if p.cur.kind != tokEOF {
		return nil, p.syntaxErrorf(p.cur.pos, "unexpected token %q", p.cur.val)
	}
	return nodes, nil
}

// nextText 推进到下一个文本模式 token。
func (p *parser) nextText() error {
	tok, err := p.lx.nextText()
	if err != nil {
		return err
	}
	p.cur = tok
	return nil
}

// nextExpr 推进到下一个表达式模式 token。
func (p *parser) nextExpr() error {
	tok, err := p.lx.nextExpr()
	if err != nil {
		return err
	}
	p.cur = tok
	return nil
}

func (p *parser) syntaxErrorf(pos Pos, format string, args ...any) error {
	return &SyntaxError{File: p.lx.file, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// parseBlock 解析节点序列，直到遇到指定的结束标签（end/else）。
// 返回节点列表与终止标签名（顶层调用时终止标签名为 ""）。
func (p *parser) parseBlock(stops map[string]bool) ([]Node, string, error) {
	var nodes []Node
	for {
		switch p.cur.kind {
		case tokText:
			nodes = append(nodes, &TextNode{Pos: p.cur.pos, Text: p.cur.val})
			if err := p.nextText(); err != nil {
				return nil, "", err
			}
		case tokEOF:
			return nodes, "", nil
		case tokOpen, tokOpenRaw:
			safe := p.cur.kind == tokOpenRaw
			pos := p.cur.pos
			if err := p.nextExpr(); err != nil {
				return nil, "", err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return nil, "", err
			}
			want := tokClose
			if safe {
				want = tokCloseRaw
			}
			if p.cur.kind != want {
				if p.cur.kind == tokEOF {
					return nil, "", p.syntaxErrorf(pos, "unterminated interpolation")
				}
				return nil, "", p.syntaxErrorf(p.cur.pos, "expected closing delimiter, got %q", p.cur.val)
			}
			nodes = append(nodes, &VarNode{Pos: pos, Expr: expr, Safe: safe})
			if err := p.nextText(); err != nil {
				return nil, "", err
			}
		case tokTagOpen:
			var term string
			var err error
			nodes, term, err = p.parseTag(nodes, stops)
			if err != nil {
				return nil, "", err
			}
			if term != "" {
				return nodes, term, nil
			}
		case tokClose, tokCloseRaw, tokTagClose:
			return nil, "", p.syntaxErrorf(p.cur.pos, "unexpected closing delimiter %q", p.cur.val)
		default:
			return nil, "", p.syntaxErrorf(p.cur.pos, "unexpected token %q", p.cur.val)
		}
	}
}

// parseTag 解析 {% ... %} 标签。若标签是块终止符（end/else），
// 返回终止标签名且不修改 nodes。
func (p *parser) parseTag(nodes []Node, stops map[string]bool) ([]Node, string, error) {
	if err := p.nextExpr(); err != nil {
		return nodes, "", err
	}
	if p.cur.kind != tokIdent {
		return nodes, "", p.syntaxErrorf(p.cur.pos, "expected tag name")
	}
	kw := p.cur.val
	pos := p.cur.pos

	if stops[kw] {
		// 消费到标签结束
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokTagClose {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "expected %%} after %s", kw)
		}
		if err := p.nextText(); err != nil {
			return nodes, "", err
		}
		return nodes, kw, nil
	}

	switch kw {
	case "if":
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokTagClose {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "expected %%} after if condition")
		}
		if err := p.nextText(); err != nil {
			return nodes, "", err
		}
		body, term, err := p.parseBlock(map[string]bool{"else": true, "end": true})
		if err != nil {
			return nodes, "", err
		}
		var elseBody []Node
		if term == "else" {
			elseBody, term, err = p.parseBlock(map[string]bool{"end": true})
			if err != nil {
				return nodes, "", err
			}
		}
		if term != "end" {
			return nodes, "", p.syntaxErrorf(pos, "missing end for if")
		}
		nodes = append(nodes, &IfNode{Pos: pos, Cond: cond, Body: body, Else: elseBody})
		return nodes, "", nil
	case "for":
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokIdent {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "expected loop variable name")
		}
		varName := p.cur.val
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokIdent || p.cur.val != "in" {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "expected %q in for tag", "in")
		}
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		iter, err := p.parseExpr()
		if err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokTagClose {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "expected %%} after for expression")
		}
		if err := p.nextText(); err != nil {
			return nodes, "", err
		}
		body, term, err := p.parseBlock(map[string]bool{"end": true})
		if err != nil {
			return nodes, "", err
		}
		if term != "end" {
			return nodes, "", p.syntaxErrorf(pos, "missing end for for")
		}
		nodes = append(nodes, &ForNode{Pos: pos, Var: varName, Iterable: iter, Body: body})
		return nodes, "", nil
	case "include":
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		if p.cur.kind != tokString {
			return nodes, "", p.syntaxErrorf(p.cur.pos, "include name must be a string literal")
		}
		name := p.cur.val
		var params []IncludeParam
		if err := p.nextExpr(); err != nil {
			return nodes, "", err
		}
		for p.cur.kind != tokTagClose {
			if p.cur.kind == tokEOF {
				return nodes, "", p.syntaxErrorf(pos, "unterminated include tag")
			}
			if p.cur.kind != tokIdent {
				return nodes, "", p.syntaxErrorf(p.cur.pos, "expected parameter name")
			}
			pName := p.cur.val
			if err := p.nextExpr(); err != nil {
				return nodes, "", err
			}
			if p.cur.kind != tokSymbol || p.cur.val != "=" {
				return nodes, "", p.syntaxErrorf(p.cur.pos, "expected = after parameter name")
			}
			if err := p.nextExpr(); err != nil {
				return nodes, "", err
			}
			val, err := p.parseExpr()
			if err != nil {
				return nodes, "", err
			}
			params = append(params, IncludeParam{Name: pName, Value: val})
			// 可选逗号
			if p.cur.kind == tokSymbol && p.cur.val == "," {
				if err := p.nextExpr(); err != nil {
					return nodes, "", err
				}
			}
		}
		if err := p.nextText(); err != nil {
			return nodes, "", err
		}
		nodes = append(nodes, &IncludeNode{Pos: pos, Name: name, Params: params})
		return nodes, "", nil
	default:
		return nodes, "", p.syntaxErrorf(p.cur.pos, "unknown tag %q", kw)
	}
}

// ---------- 表达式解析（Pratt） ----------

var binPrec = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3,
	"<": 4, ">": 4, "<=": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func (p *parser) parseExpr() (Expr, error) {
	return p.parseBinary(0)
}

func (p *parser) parseBinary(minPrec int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return Expr{}, err
	}
	for {
		op := ""
		if p.cur.kind == tokSymbol {
			op = p.cur.val
		} else if p.cur.kind == tokIdent {
			switch p.cur.val {
			case "and":
				op = "&&"
			case "or":
				op = "||"
			}
		}
		prec, ok := binPrec[op]
		if !ok || prec < minPrec {
			break
		}
		opPos := p.cur.pos
		if err := p.nextExpr(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseBinary(prec + 1)
		if err != nil {
			return Expr{}, err
		}
		left = Expr{Kind: ExprBinary, Pos: opPos, Op: op, Args: []Expr{left, right}}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	op := ""
	if p.cur.kind == tokSymbol && (p.cur.val == "!" || p.cur.val == "-") {
		op = p.cur.val
	} else if p.cur.kind == tokIdent && p.cur.val == "not" {
		op = "!"
	}
	if op != "" {
		pos := p.cur.pos
		if err := p.nextExpr(); err != nil {
			return Expr{}, err
		}
		operand, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprUnary, Pos: pos, Op: op, Args: []Expr{operand}}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	pos := p.cur.pos
	switch p.cur.kind {
	case tokNumber:
		val := p.cur.val
		if err := p.nextExpr(); err != nil {
			return Expr{}, err
		}
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return Expr{Kind: ExprLit, Tag: litInt, Pos: pos, Int: i}, nil
		}
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return Expr{}, p.syntaxErrorf(pos, "invalid number %q", val)
		}
		return Expr{Kind: ExprLit, Tag: litFloat, Pos: pos, Float: f}, nil
	case tokString:
		val := p.cur.val
		if err := p.nextExpr(); err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprLit, Tag: litStr, Pos: pos, Str: val}, nil
	case tokOpen:
		// 不应该在表达式模式出现
		return Expr{}, p.syntaxErrorf(pos, "unexpected token")
	case tokIdent:
		name := p.cur.val
		switch name {
		case "true":
			if err := p.nextExpr(); err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprLit, Tag: litBool, Pos: pos, Bool: true}, nil
		case "false":
			if err := p.nextExpr(); err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprLit, Tag: litBool, Pos: pos, Bool: false}, nil
		case "nil":
			if err := p.nextExpr(); err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprNil, Pos: pos}, nil
		}
		if err := p.nextExpr(); err != nil {
			return Expr{}, err
		}
		// 函数调用
		if p.cur.kind == tokSymbol && p.cur.val == "(" {
			args, err := p.parseArgs()
			if err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprCall, Pos: pos, Func: name, Args: args}, nil
		}
		// 变量路径：a.b[0].c
		segs := []PathSeg{{Name: name}}
		for {
			if p.cur.kind == tokSymbol && p.cur.val == "." {
				if err := p.nextExpr(); err != nil {
					return Expr{}, err
				}
				if p.cur.kind != tokIdent {
					return Expr{}, p.syntaxErrorf(p.cur.pos, "expected field name after '.'")
				}
				segs = append(segs, PathSeg{Name: p.cur.val})
				if err := p.nextExpr(); err != nil {
					return Expr{}, err
				}
			} else if p.cur.kind == tokSymbol && p.cur.val == "[" {
				if err := p.nextExpr(); err != nil {
					return Expr{}, err
				}
				idx, err := p.parseExpr()
				if err != nil {
					return Expr{}, err
				}
				if p.cur.kind != tokSymbol && p.cur.val != "]" {
					return Expr{}, p.syntaxErrorf(p.cur.pos, "expected ]")
				}
				if err := p.nextExpr(); err != nil {
					return Expr{}, err
				}
				segs = append(segs, PathSeg{Index: &idx})
			} else {
				break
			}
		}
		return Expr{Kind: ExprPath, Pos: pos, Path: segs}, nil
	case tokSymbol:
		if p.cur.val == "(" {
			if err := p.nextExpr(); err != nil {
				return Expr{}, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return Expr{}, err
			}
			if p.cur.kind != tokSymbol || p.cur.val != ")" {
				return Expr{}, p.syntaxErrorf(p.cur.pos, "expected )")
			}
			if err := p.nextExpr(); err != nil {
				return Expr{}, err
			}
			return expr, nil
		}
		return Expr{}, p.syntaxErrorf(pos, "unexpected token %q", p.cur.val)
	default:
		return Expr{}, p.syntaxErrorf(pos, "expected expression, got %q", p.cur.val)
	}
}

// parseArgs 解析函数调用的参数列表，进入时 cur 为 '('，离开时 cur 为 ')' 之后。
func (p *parser) parseArgs() ([]Expr, error) {
	if err := p.nextExpr(); err != nil {
		return nil, err
	}
	var args []Expr
	if p.cur.kind == tokSymbol && p.cur.val == ")" {
		if err := p.nextExpr(); err != nil {
			return nil, err
		}
		return args, nil
	}
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.cur.kind == tokSymbol && p.cur.val == "," {
			if err := p.nextExpr(); err != nil {
				return nil, err
			}
			continue
		}
		if p.cur.kind != tokSymbol || p.cur.val != ")" {
			return nil, p.syntaxErrorf(p.cur.pos, "expected , or ) in argument list")
		}
		if err := p.nextExpr(); err != nil {
			return nil, err
		}
		return args, nil
	}
}
