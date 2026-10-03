package template

import (
	"fmt"
	"strings"
)

// tokenKind 词法单元类型。
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokText
	tokOutput    // {{ expr }}  转义输出
	tokOutputRaw // {{{ expr }}} 不转义输出
	tokStmt      // {% ... %} 语句
	tokIdent
	tokNumber
	tokString
	tokPunct
)

type token struct {
	kind tokenKind
	val  string
	pos  Pos
}

// lex 将模板源码切分为文本与标签 token 流。
func lex(src string) ([]token, error) {
	var toks []token
	line, col := 1, 1
	pos := func() Pos { return Pos{Line: line, Col: col} }
	advance := func(s string) {
		for _, r := range s {
			if r == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
	}
	i := 0
	for i < len(src) {
		// 找下一个标签起始
		j := strings.IndexAny(src[i:], "{")
		if j < 0 {
			if i < len(src) {
				toks = append(toks, token{tokText, src[i:], pos()})
			}
			break
		}
		j += i
		if j+1 >= len(src) || (src[j+1] != '{' && src[j+1] != '%') {
			// 普通 '{' 字符，归入文本
			toks = append(toks, token{tokText, src[i : j+1], pos()})
			advance(src[i : j+1])
			i = j + 1
			continue
		}
		if j > i {
			toks = append(toks, token{tokText, src[i:j], pos()})
			advance(src[i:j])
		}
		start := pos()
		if src[j+1] == '%' {
			end := strings.Index(src[j+2:], "%}")
			if end < 0 {
				return nil, &SyntaxError{Pos: start, Msg: "unclosed {% tag}"}
			}
			inner := src[j+2 : j+2+end]
			toks = append(toks, token{tokStmt, inner, Pos{start.Line, start.Col + 2}})
			advance(src[j : j+2+end+2])
			i = j + 2 + end + 2
			continue
		}
		// {{ 或 {{{
		raw := false
		openLen := 2
		if j+2 < len(src) && src[j+2] == '{' {
			raw = true
			openLen = 3
		}
		closeTag := "}}"
		if raw {
			closeTag = "}}}"
		}
		end := strings.Index(src[j+openLen:], closeTag)
		if end < 0 {
			return nil, &SyntaxError{Pos: start, Msg: fmt.Sprintf("unclosed %s tag", src[j:j+openLen])}
		}
		inner := src[j+openLen : j+openLen+end]
		kind := tokOutput
		if raw {
			kind = tokOutputRaw
		}
		toks = append(toks, token{kind, inner, Pos{start.Line, start.Col + openLen}})
		advance(src[j : j+openLen+end+len(closeTag)])
		i = j + openLen + end + len(closeTag)
	}
	toks = append(toks, token{kind: tokEOF, pos: pos()})
	return toks, nil
}

// isIdentStart 判断标识符起始字符。
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// lexExpr 将标签内的表达式源码切分为表达式 token 流。
// base 为表达式在模板中的起始位置，用于错误定位。
func lexExpr(src string, base Pos) ([]token, error) {
	var toks []token
	i := 0
	line, col := base.Line, base.Col
	pos := func() Pos { return Pos{Line: line, Col: col} }
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if c == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		case isIdentStart(c):
			start := i
			p := pos()
			for i < len(src) && (isIdentStart(src[i]) || isDigit(src[i])) {
				i++
				col++
			}
			toks = append(toks, token{tokIdent, src[start:i], p})
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			start := i
			p := pos()
			for i < len(src) && (isDigit(src[i]) || src[i] == '.') {
				i++
				col++
			}
			toks = append(toks, token{tokNumber, src[start:i], p})
		case c == '"' || c == '\'':
			p := pos()
			i++
			col++
			var sb strings.Builder
			closed := false
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					switch src[i+1] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					default:
						sb.WriteByte(src[i+1])
					}
					i += 2
					col += 2
					continue
				}
				if src[i] == c {
					closed = true
					i++
					col++
					break
				}
				sb.WriteByte(src[i])
				i++
				col++
			}
			if !closed {
				return nil, &SyntaxError{Pos: p, Msg: "unterminated string literal"}
			}
			toks = append(toks, token{tokString, sb.String(), p})
		default:
			p := pos()
			// 双字符运算符
			if i+1 < len(src) {
				two := src[i : i+2]
				switch two {
				case "==", "!=", "<=", ">=", "&&", "||":
					toks = append(toks, token{tokPunct, two, p})
					i += 2
					col += 2
					continue
				}
			}
			switch c {
			case '.', ',', '(', ')', '[', ']', '<', '>', '+', '-', '*', '/', '%', '=', '!':
				toks = append(toks, token{tokPunct, string(c), p})
				i++
				col++
			default:
				return nil, &SyntaxError{Pos: p, Msg: fmt.Sprintf("unexpected character %q", string(c))}
			}
		}
	}
	toks = append(toks, token{kind: tokEOF, pos: pos()})
	return toks, nil
}

// advancePos 按字符串内容推进位置（处理换行）。
func advancePos(p Pos, s string) Pos {
	for _, r := range s {
		if r == '\n' {
			p.Line++
			p.Col = 1
		} else {
			p.Col++
		}
	}
	return p
}
