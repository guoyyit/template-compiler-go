package template

import (
	"encoding/json"
	"fmt"
)

// FormatVersion 编译产物格式版本。格式不兼容变更时递增。
const FormatVersion = 1

const formatMagic = "tplc"

// ---- 序列化 DTO ----

type artifact struct {
	Magic   string              `json:"magic"`
	Version int                 `json:"version"`
	Name    string              `json:"name"`
	Progs   map[string]sProgram `json:"progs"`
}

type sProgram struct {
	Body  []sInstr `json:"body"`
	Exprs []sExpr  `json:"exprs"`
}

type sInstr struct {
	Op       string         `json:"op"`
	Text     string         `json:"text,omitempty"`
	ExprIdx  int            `json:"exprIdx,omitempty"`
	Escape   bool           `json:"escape,omitempty"`
	Branches []sBranch      `json:"branches,omitempty"`
	KeyVar   string         `json:"keyVar,omitempty"`
	ValVar   string         `json:"valVar,omitempty"`
	IterIdx  int            `json:"iterIdx,omitempty"`
	Body     []sInstr       `json:"body,omitempty"`
	CallName string         `json:"callName,omitempty"`
	Params   map[string]int `json:"params,omitempty"`
}

type sBranch struct {
	CondIdx int      `json:"condIdx"`
	Body    []sInstr `json:"body"`
}

// sExpr 表达式 DTO。子表达式：unary/field 用 A；binary/index 用 A、B。
type sExpr struct {
	Kind    string `json:"kind"`
	ValKind string `json:"valKind,omitempty"` // lit 的类型标签: int/float/string/bool/nil
	Val     any    `json:"val,omitempty"`
	Name    string `json:"name,omitempty"`
	Op      string `json:"op,omitempty"`
	A       *sExpr `json:"a,omitempty"`
	B       *sExpr `json:"b,omitempty"`
}

// Marshal 将编译产物序列化为可跨进程传输/落盘的字节流。
func (t *Template) Marshal() ([]byte, error) {
	a := artifact{Magic: formatMagic, Version: FormatVersion, Name: t.name, Progs: map[string]sProgram{}}
	for name, p := range t.progs {
		sp := sProgram{Body: instrsToS(p.body)}
		for _, e := range p.exprs {
			sp.Exprs = append(sp.Exprs, exprToS(e))
		}
		a.Progs[name] = sp
	}
	return json.Marshal(a)
}

// Unmarshal 反序列化编译产物。
// 版本不兼容或数据损坏时返回 *VersionError，调用方应回退到重新编译。
func Unmarshal(data []byte) (*Template, error) {
	var a artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, &VersionError{Msg: "corrupt artifact: " + err.Error()}
	}
	if a.Magic != formatMagic {
		return nil, &VersionError{Msg: "not a template artifact"}
	}
	if a.Version != FormatVersion {
		return nil, &VersionError{Got: a.Version, Want: FormatVersion}
	}
	t := &Template{name: a.Name, progs: map[string]*program{}}
	for name, sp := range a.Progs {
		p := &program{name: name}
		for i := range sp.Exprs {
			e, err := exprFromS(&sp.Exprs[i])
			if err != nil {
				return nil, &VersionError{Msg: fmt.Sprintf("corrupt artifact: %v", err)}
			}
			p.exprs = append(p.exprs, e)
		}
		body, err := instrsFromS(sp.Body, len(p.exprs))
		if err != nil {
			return nil, &VersionError{Msg: fmt.Sprintf("corrupt artifact: %v", err)}
		}
		p.body = body
		t.progs[name] = p
	}
	if _, ok := t.progs[t.name]; !ok {
		return nil, &VersionError{Msg: "corrupt artifact: entry template missing"}
	}
	return t, nil
}

var opNames = map[opCode]string{
	opText: "text", opOutput: "output", opIf: "if", opFor: "for", opCall: "call",
}

var opByName = map[string]opCode{
	"text": opText, "output": opOutput, "if": opIf, "for": opFor, "call": opCall,
}

func instrsToS(in []instr) []sInstr {
	out := make([]sInstr, 0, len(in))
	for _, ins := range in {
		si := sInstr{
			Op:       opNames[ins.op],
			Text:     ins.text,
			ExprIdx:  ins.exprIdx,
			Escape:   ins.escape,
			KeyVar:   ins.keyVar,
			ValVar:   ins.valVar,
			IterIdx:  ins.iterIdx,
			Body:     instrsToS(ins.body),
			CallName: ins.callName,
			Params:   ins.callParams,
		}
		for _, br := range ins.branches {
			si.Branches = append(si.Branches, sBranch{CondIdx: br.condIdx, Body: instrsToS(br.body)})
		}
		out = append(out, si)
	}
	return out
}

func instrsFromS(in []sInstr, nExprs int) ([]instr, error) {
	out := make([]instr, 0, len(in))
	checkIdx := func(idx int) error {
		if idx < 0 || idx >= nExprs {
			return fmt.Errorf("expression index %d out of range", idx)
		}
		return nil
	}
	for _, si := range in {
		op, ok := opByName[si.Op]
		if !ok {
			return nil, fmt.Errorf("unknown op %q", si.Op)
		}
		ins := instr{
			op:      op,
			text:    si.Text,
			exprIdx: si.ExprIdx,
			escape:  si.Escape,
			keyVar:  si.KeyVar,
			valVar:  si.ValVar,
			iterIdx: si.IterIdx,
		}
		switch op {
		case opOutput:
			if err := checkIdx(si.ExprIdx); err != nil {
				return nil, err
			}
		case opIf:
			for _, sb := range si.Branches {
				if sb.CondIdx >= 0 {
					if err := checkIdx(sb.CondIdx); err != nil {
						return nil, err
					}
				}
				body, err := instrsFromS(sb.Body, nExprs)
				if err != nil {
					return nil, err
				}
				ins.branches = append(ins.branches, branch{condIdx: sb.CondIdx, body: body})
			}
		case opFor:
			if err := checkIdx(si.IterIdx); err != nil {
				return nil, err
			}
			body, err := instrsFromS(si.Body, nExprs)
			if err != nil {
				return nil, err
			}
			ins.body = body
		case opCall:
			ins.callName = si.CallName
			ins.callParams = map[string]int{}
			for k, idx := range si.Params {
				if err := checkIdx(idx); err != nil {
					return nil, err
				}
				ins.callParams[k] = idx
			}
		}
		out = append(out, ins)
	}
	return out, nil
}

func exprToS(e expr) sExpr {
	switch n := e.(type) {
	case *litExpr:
		se := sExpr{Kind: "lit"}
		switch v := n.val.(type) {
		case nil:
			se.ValKind = "nil"
		case bool:
			se.ValKind, se.Val = "bool", v
		case string:
			se.ValKind, se.Val = "string", v
		case int64:
			se.ValKind, se.Val = "int", v
		case float64:
			se.ValKind, se.Val = "float", v
		default:
			se.ValKind, se.Val = "float", toFloatMust(v)
		}
		return se
	case *identExpr:
		return sExpr{Kind: "ident", Name: n.name}
	case *fieldExpr:
		a := exprToS(n.obj)
		return sExpr{Kind: "field", Name: n.name, A: &a}
	case *indexExpr:
		a, b := exprToS(n.obj), exprToS(n.index)
		return sExpr{Kind: "index", A: &a, B: &b}
	case *unaryExpr:
		a := exprToS(n.x)
		return sExpr{Kind: "unary", Op: n.op, A: &a}
	case *binaryExpr:
		a, b := exprToS(n.left), exprToS(n.right)
		return sExpr{Kind: "binary", Op: n.op, A: &a, B: &b}
	}
	return sExpr{Kind: "lit", ValKind: "nil"}
}

func toFloatMust(v any) float64 {
	f, _ := toFloat(v)
	return f
}

func exprFromS(se *sExpr) (expr, error) {
	if se == nil {
		return nil, fmt.Errorf("null expression")
	}
	switch se.Kind {
	case "lit":
		switch se.ValKind {
		case "nil":
			return &litExpr{val: nil}, nil
		case "bool":
			b, ok := se.Val.(bool)
			if !ok {
				return nil, fmt.Errorf("bad bool literal")
			}
			return &litExpr{val: b}, nil
		case "string":
			s, ok := se.Val.(string)
			if !ok {
				return nil, fmt.Errorf("bad string literal")
			}
			return &litExpr{val: s}, nil
		case "int":
			f, ok := se.Val.(float64)
			if !ok {
				return nil, fmt.Errorf("bad int literal")
			}
			return &litExpr{val: int64(f)}, nil
		case "float":
			f, ok := se.Val.(float64)
			if !ok {
				return nil, fmt.Errorf("bad float literal")
			}
			return &litExpr{val: f}, nil
		}
		return nil, fmt.Errorf("unknown literal kind %q", se.ValKind)
	case "ident":
		return &identExpr{name: se.Name}, nil
	case "field":
		a, err := exprFromS(se.A)
		if err != nil {
			return nil, err
		}
		return &fieldExpr{obj: a, name: se.Name}, nil
	case "index":
		a, err := exprFromS(se.A)
		if err != nil {
			return nil, err
		}
		b, err := exprFromS(se.B)
		if err != nil {
			return nil, err
		}
		return &indexExpr{obj: a, index: b}, nil
	case "unary":
		a, err := exprFromS(se.A)
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: se.Op, x: a}, nil
	case "binary":
		a, err := exprFromS(se.A)
		if err != nil {
			return nil, err
		}
		b, err := exprFromS(se.B)
		if err != nil {
			return nil, err
		}
		return &binaryExpr{op: se.Op, left: a, right: b}, nil
	}
	return nil, fmt.Errorf("unknown expression kind %q", se.Kind)
}
