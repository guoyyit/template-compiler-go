package engine

import (
	"fmt"
	"strings"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokText
	tokOpen      // {{
	tokOpenRaw   // {{{
	tokClose     // }}
	tokCloseRaw  // }}}
	tokTagOpen   // {%
	tokTagClose  // %}
	tokIdent
	tokNumber
	tokString
	tokSymbol
)

type token struct {
	kind tokKind
	pos  Pos
	val  string
}

// lexer 将模板文本切分为 token。nextText 工作在文本模式，
// nextExpr 工作在表达式模式，由 parser 驱动切换。
type lexer struct {
	src  string
	file string
	pos  int
	line int
	col  int
}

func newLexer(src, file string) *lexer {
	return &lexer{src: src, file: file, line: 1, col: 1}
}

func (l *lexer) errorf(pos Pos, msg string) error {
	return &SyntaxError{File: l.file, Pos: pos, Msg: msg}
}

func (l *lexer) peekByte() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

// skipSpaces 跳过表达式模式下的空白。
func (l *lexer) skipSpaces() {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.advance()
		default:
			return
		}
	}
}

// nextText 扫描一段静态文本，或返回动作起始符（{{ / {%）。
// 注释 {# ... #} 在文本模式下直接跳过。
func (l *lexer) nextText() (token, error) {
	start := l.pos
	startPos := Pos{Line: l.line, Col: l.col}
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == '{' && l.pos+1 < len(l.src) {
			switch l.src[l.pos+1] {
			case '{':
				if start < l.pos {
					return token{kind: tokText, pos: startPos, val: l.src[start:l.pos]}, nil
				}
				p := Pos{Line: l.line, Col: l.col}
				l.advance() // {
				l.advance() // {
				if l.peekByte() == '{' {
					l.advance()
					return token{kind: tokOpenRaw, pos: p}, nil
				}
				return token{kind: tokOpen, pos: p}, nil
			case '%':
				if start < l.pos {
					return token{kind: tokText, pos: startPos, val: l.src[start:l.pos]}, nil
				}
				p := Pos{Line: l.line, Col: l.col}
				l.advance() // {
				l.advance() // %
				return token{kind: tokTagOpen, pos: p}, nil
			case '#':
				if start < l.pos {
					return token{kind: tokText, pos: startPos, val: l.src[start:l.pos]}, nil
				}
				// 跳过注释直到 #}
				p := Pos{Line: l.line, Col: l.col}
				l.advance() // {
				l.advance() // #
				closed := false
				for l.pos < len(l.src) {
					if l.src[l.pos] == '#' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '}' {
						l.advance()
						l.advance()
						closed = true
						break
					}
					l.advance()
				}
				if !closed {
					return token{}, l.errorf(p, "unterminated comment")
				}
				start = l.pos
				startPos = Pos{Line: l.line, Col: l.col}
			default:
				l.advance()
			}
		} else {
			l.advance()
		}
	}
	if start < l.pos {
		return token{kind: tokText, pos: startPos, val: l.src[start:l.pos]}, nil
	}
	return token{kind: tokEOF, pos: Pos{Line: l.line, Col: l.col}}, nil
}

// nextExpr 扫描表达式 token，直到遇到动作结束符（}} / }% 对应 %}）。
func (l *lexer) nextExpr() (token, error) {
	l.skipSpaces()
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, pos: Pos{Line: l.line, Col: l.col}}, nil
	}
	p := Pos{Line: l.line, Col: l.col}
	ch := l.src[l.pos]

	// 结束符（%} 必须在 % 之前识别）
	if ch == '%' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '}' {
		l.advance()
		l.advance()
		return token{kind: tokTagClose, pos: p}, nil
	}
	if ch == '}' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '}' {
		l.advance()
		l.advance()
		if l.peekByte() == '}' {
			l.advance()
			return token{kind: tokCloseRaw, pos: p}, nil
		}
		return token{kind: tokClose, pos: p}, nil
	}

	switch {
	case ch == '"' || ch == '\'':
		return l.scanString(p)
	case isDigit(ch):
		return l.scanNumber(p)
	case isIdentStart(ch):
		return l.scanIdent(p)
	}

	// 符号
	two := ""
	if l.pos+1 < len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case "==", "!=", "<=", ">=", "&&", "||":
		l.advance()
		l.advance()
		return token{kind: tokSymbol, pos: p, val: two}, nil
	}
	switch ch {
	case '+', '-', '*', '/', '%', '<', '>', '!', '.', ',', '(', ')', '[', ']', '=':
		l.advance()
		return token{kind: tokSymbol, pos: p, val: string(ch)}, nil
	}
	return token{}, l.errorf(p, fmt.Sprintf("unexpected character %q", ch))
}

func (l *lexer) scanString(p Pos) (token, error) {
	quote := l.advance() // 消费引号
	var sb strings.Builder
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if ch == quote {
			l.advance()
			return token{kind: tokString, pos: p, val: sb.String()}, nil
		}
		if ch == '\\' {
			l.advance()
			if l.pos >= len(l.src) {
				break
			}
			esc := l.advance()
			switch esc {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			case '\'':
				sb.WriteByte('\'')
			default:
				sb.WriteByte(esc)
			}
			continue
		}
		if ch == '\n' {
			return token{}, l.errorf(p, "unterminated string")
		}
		sb.WriteByte(l.advance())
	}
	return token{}, l.errorf(p, "unterminated string")
}

func (l *lexer) scanNumber(p Pos) (token, error) {
	start := l.pos
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if isDigit(ch) {
			l.advance()
		} else if ch == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
			l.advance()
		} else {
			break
		}
	}
	return token{kind: tokNumber, pos: p, val: l.src[start:l.pos]}, nil
}

func (l *lexer) scanIdent(p Pos) (token, error) {
	start := l.pos
	for l.pos < len(l.src) {
		ch := l.src[l.pos]
		if isIdentPart(ch) {
			l.advance()
		} else {
			break
		}
	}
	return token{kind: tokIdent, pos: p, val: l.src[start:l.pos]}, nil
}

func isDigit(ch byte) bool      { return ch >= '0' && ch <= '9' }
func isIdentStart(ch byte) bool { return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') }
func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}
