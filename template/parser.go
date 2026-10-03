package template

import (
	"fmt"
	"strconv"
	"strings"
)

// parser 模板语句解析器。
type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) atEnd() bool { return p.peek().kind == tokEOF }

// parse 将模板 token 流解析为语法树。
func parse(toks []token) ([]node, error) {
	p := &parser{toks: toks}
	body, term, err := p.parseBody("")
	if err != nil {
		return nil, err
	}
	if term != "" {
		return nil, &SyntaxError{Pos: p.peek().pos, Msg: fmt.Sprintf("unexpected %q without matching opener", term)}
	}
	return body, nil
}

// stmtKeyword 返回语句 token 的首个关键字。
func stmtKeyword(t token) string {
	if t.kind != tokStmt {
		return ""
	}
	kw := strings.TrimSpace(t.val)
	if i := strings.IndexAny(kw, " \t"); i >= 0 {
		kw = kw[:i]
	}
	return kw
}

// parseBody 解析节点序列，直到遇到 terminators 中的语句关键字或 EOF。
// 返回终止关键字（EOF 时为空串）。
func (p *parser) parseBody(terminators ...string) ([]node, string, error) {
	var body []node
	for !p.atEnd() {
		t := p.peek()
		if t.kind == tokStmt {
			kw := stmtKeyword(t)
			for _, term := range terminators {
				if kw == term {
					return body, kw, nil
				}
			}
		}
		n, err := p.parseNode()
		if err != nil {
			return nil, "", err
		}
		if n != nil {
			body = append(body, n)
		}
	}
	return body, "", nil
}

func (p *parser) parseNode() (node, error) {
	t := p.next()
	switch t.kind {
	case tokText:
		return &textNode{pos: t.pos, val: t.val}, nil
	case tokOutput, tokOutputRaw:
		e, err := parseTagExpr(t)
		if err != nil {
			return nil, err
		}
		return &outputNode{pos: t.pos, expr: e, escape: t.kind == tokOutput}, nil
	case tokStmt:
		return p.parseStmt(t)
	default:
		return nil, &SyntaxError{Pos: t.pos, Msg: "unexpected token"}
	}
}

// parseTagExpr 解析输出标签内的表达式。
func parseTagExpr(t token) (expr, error) {
	if strings.TrimSpace(t.val) == "" {
		return nil, &SyntaxError{Pos: t.pos, Msg: "empty expression"}
	}
	etoks, err := lexExpr(t.val, t.pos)
	if err != nil {
		return nil, err
	}
	return parseExpr(etoks)
}

// stmtRest 拆分语句为关键字与剩余部分，并给出剩余部分在模板中的位置。
func stmtRest(t token) (kw, rest string, restPos Pos) {
	trimmed := strings.TrimSpace(t.val)
	kw = trimmed
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		kw = trimmed[:i]
	}
	rest = strings.TrimSpace(trimmed[len(kw):])
	restPos = t.pos
	if rest != "" {
		if off := strings.Index(t.val, rest); off >= 0 {
			restPos = advancePos(t.pos, t.val[:off])
		}
	}
	return kw, rest, restPos
}

func (p *parser) parseStmt(t token) (node, error) {
	kw, rest, restPos := stmtRest(t)
	switch kw {
	case "if":
		return p.parseIf(t, rest, restPos)
	case "for":
		return p.parseFor(t, rest, restPos)
	case "include":
		return p.parseInclude(t, rest, restPos)
	case "elif", "else", "endif", "endfor":
		return nil, &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("unexpected %q without matching opener", kw)}
	default:
		return nil, &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("unknown statement %q", kw)}
	}
}

func (p *parser) parseIf(t token, condSrc string, condPos Pos) (node, error) {
	if condSrc == "" {
		return nil, &SyntaxError{Pos: t.pos, Msg: "if requires a condition"}
	}
	n := &ifNode{pos: t.pos}
	cond, err := parseTagExpr(token{kind: tokOutput, val: condSrc, pos: condPos})
	if err != nil {
		return nil, err
	}
	body, term, err := p.parseBody("elif", "else", "endif")
	if err != nil {
		return nil, err
	}
	n.branches = append(n.branches, ifBranch{cond: cond, body: body})
	for term == "elif" {
		et := p.next()
		_, erest, erestPos := stmtRest(et)
		if erest == "" {
			return nil, &SyntaxError{Pos: et.pos, Msg: "elif requires a condition"}
		}
		c, err := parseTagExpr(token{kind: tokOutput, val: erest, pos: erestPos})
		if err != nil {
			return nil, err
		}
		b, term2, err := p.parseBody("elif", "else", "endif")
		if err != nil {
			return nil, err
		}
		n.branches = append(n.branches, ifBranch{cond: c, body: b})
		term = term2
	}
	if term == "else" {
		p.next()
		b, term2, err := p.parseBody("endif")
		if err != nil {
			return nil, err
		}
		n.branches = append(n.branches, ifBranch{cond: nil, body: b})
		term = term2
	}
	if term != "endif" {
		return nil, &SyntaxError{Pos: t.pos, Msg: "if without matching endif"}
	}
	p.next() // consume endif
	return n, nil
}

func (p *parser) parseFor(t token, rest string, restPos Pos) (node, error) {
	// 形如: x in expr  或  k, v in expr
	etoks, err := lexExpr(rest, restPos)
	if err != nil {
		return nil, err
	}
	ep := &exprParser{toks: etoks}
	n := &forNode{pos: t.pos}
	v1 := ep.next()
	if v1.kind != tokIdent {
		return nil, &SyntaxError{Pos: v1.pos, Msg: "for requires a loop variable"}
	}
	n.valVar = v1.val
	if ep.peek().kind == tokPunct && ep.peek().val == "," {
		ep.next()
		v2 := ep.next()
		if v2.kind != tokIdent {
			return nil, &SyntaxError{Pos: v2.pos, Msg: "for requires a value variable after ','"}
		}
		n.keyVar, n.valVar = v1.val, v2.val
	}
	inTok := ep.next()
	if inTok.kind != tokIdent || inTok.val != "in" {
		return nil, &SyntaxError{Pos: inTok.pos, Msg: "for requires 'in' after loop variable"}
	}
	iter, err := ep.parseOr()
	if err != nil {
		return nil, err
	}
	if ep.peek().kind != tokEOF {
		return nil, &SyntaxError{Pos: ep.peek().pos, Msg: "unexpected tokens after for expression"}
	}
	n.iter = iter
	body, term, err := p.parseBody("endfor")
	if err != nil {
		return nil, err
	}
	if term != "endfor" {
		return nil, &SyntaxError{Pos: t.pos, Msg: "for without matching endfor"}
	}
	p.next() // consume endfor
	n.body = body
	return n, nil
}

func (p *parser) parseInclude(t token, rest string, restPos Pos) (node, error) {
	etoks, err := lexExpr(rest, restPos)
	if err != nil {
		return nil, err
	}
	ep := &exprParser{toks: etoks}
	nameTok := ep.next()
	if nameTok.kind != tokString {
		return nil, &SyntaxError{Pos: nameTok.pos, Msg: "include requires a quoted template name"}
	}
	n := &includeNode{pos: t.pos, name: nameTok.val, params: map[string]expr{}}
	if ep.peek().kind == tokIdent && ep.peek().val == "with" {
		ep.next()
		for {
			k := ep.next()
			if k.kind != tokIdent {
				return nil, &SyntaxError{Pos: k.pos, Msg: "include parameter name expected"}
			}
			eq := ep.next()
			if eq.kind != tokPunct || eq.val != "=" {
				return nil, &SyntaxError{Pos: eq.pos, Msg: "expected '=' after include parameter name"}
			}
			v, err := ep.parseOr()
			if err != nil {
				return nil, err
			}
			n.params[k.val] = v
			if ep.peek().kind == tokPunct && ep.peek().val == "," {
				ep.next()
				continue
			}
			break
		}
	}
	if ep.peek().kind != tokEOF {
		return nil, &SyntaxError{Pos: ep.peek().pos, Msg: "unexpected tokens in include statement"}
	}
	return n, nil
}

// ---- 表达式解析 ----

type exprParser struct {
	toks []token
	i    int
}

func (p *exprParser) peek() token { return p.toks[p.i] }
func (p *exprParser) next() token { t := p.toks[p.i]; p.i++; return t }

// parseExpr 解析完整表达式并校验无多余 token。
func parseExpr(toks []token) (expr, error) {
	p := &exprParser{toks: toks}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, &SyntaxError{Pos: p.peek().pos, Msg: fmt.Sprintf("unexpected token %q after expression", p.peek().val)}
	}
	return e, nil
}

func (p *exprParser) parseOr() (expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && p.peek().val == "or" ||
		p.peek().kind == tokPunct && p.peek().val == "||" {
		op := p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &binaryExpr{pos: op.pos, op: "or", left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokIdent && p.peek().val == "and" ||
		p.peek().kind == tokPunct && p.peek().val == "&&" {
		op := p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &binaryExpr{pos: op.pos, op: "and", left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseNot() (expr, error) {
	t := p.peek()
	if (t.kind == tokIdent && t.val == "not") || (t.kind == tokPunct && t.val == "!") {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{pos: t.pos, op: "not", x: x}, nil
	}
	return p.parseComparison()
}

func (p *exprParser) parseComparison() (expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.kind == tokPunct {
		switch t.val {
		case "==", "!=", "<", "<=", ">", ">=":
			p.next()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			return &binaryExpr{pos: t.pos, op: t.val, left: left, right: right}, nil
		}
	}
	return left, nil
}

func (p *exprParser) parseAdditive() (expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPunct && (p.peek().val == "+" || p.peek().val == "-") {
		op := p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &binaryExpr{pos: op.pos, op: op.val, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseMultiplicative() (expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPunct && (p.peek().val == "*" || p.peek().val == "/" || p.peek().val == "%") {
		op := p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binaryExpr{pos: op.pos, op: op.val, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseUnary() (expr, error) {
	t := p.peek()
	if t.kind == tokPunct && t.val == "-" {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{pos: t.pos, op: "-", x: x}, nil
	}
	return p.parsePostfix()
}

func (p *exprParser) parsePostfix() (expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tokPunct {
			return e, nil
		}
		switch t.val {
		case ".":
			p.next()
			name := p.next()
			if name.kind != tokIdent {
				return nil, &SyntaxError{Pos: name.pos, Msg: "expected field name after '.'"}
			}
			e = &fieldExpr{pos: t.pos, obj: e, name: name.val}
		case "[":
			p.next()
			idx, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			close := p.next()
			if close.kind != tokPunct || close.val != "]" {
				return nil, &SyntaxError{Pos: close.pos, Msg: "expected ']'"}
			}
			e = &indexExpr{pos: t.pos, obj: e, index: idx}
		default:
			return e, nil
		}
	}
}

func (p *exprParser) parsePrimary() (expr, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		if strings.Contains(t.val, ".") {
			f, err := strconv.ParseFloat(t.val, 64)
			if err != nil {
				return nil, &SyntaxError{Pos: t.pos, Msg: "invalid number literal"}
			}
			return &litExpr{pos: t.pos, val: f}, nil
		}
		n, err := strconv.ParseInt(t.val, 10, 64)
		if err != nil {
			return nil, &SyntaxError{Pos: t.pos, Msg: "invalid number literal"}
		}
		return &litExpr{pos: t.pos, val: n}, nil
	case tokString:
		return &litExpr{pos: t.pos, val: t.val}, nil
	case tokIdent:
		switch t.val {
		case "true":
			return &litExpr{pos: t.pos, val: true}, nil
		case "false":
			return &litExpr{pos: t.pos, val: false}, nil
		case "nil":
			return &litExpr{pos: t.pos, val: nil}, nil
		}
		return &identExpr{pos: t.pos, name: t.val}, nil
	case tokPunct:
		if t.val == "(" {
			e, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			close := p.next()
			if close.kind != tokPunct || close.val != ")" {
				return nil, &SyntaxError{Pos: close.pos, Msg: "expected ')'"}
			}
			return e, nil
		}
	}
	if t.kind == tokEOF {
		return nil, &SyntaxError{Pos: t.pos, Msg: "unexpected end of expression"}
	}
	return nil, &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("unexpected token %q in expression", t.val)}
}
