package template

import (
	"fmt"
	"html"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// scope 是渲染期的变量作用域链。
type scope struct {
	vars   map[string]any
	parent *scope
}

func (s *scope) get(name string) (any, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// Render 以 data 渲染入口模板。data 通常为 map[string]any；
// 其他类型会通过 "." 变量访问。
func (t *Template) Render(data any) (string, error) {
	root := &scope{vars: map[string]any{}}
	if m, ok := data.(map[string]any); ok {
		root.vars = m
	} else {
		root.vars["."] = data
	}
	var sb strings.Builder
	entry, ok := t.progs[t.name]
	if !ok {
		return "", &RenderError{Template: t.name, Path: "", Msg: "entry template not compiled"}
	}
	if err := t.exec(entry, entry.body, root, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// exec 执行一段指令序列，渲染失败返回携带模板名与数据路径的 RenderError。
func (t *Template) exec(prog *program, body []instr, sc *scope, sb *strings.Builder) error {
	for i := range body {
		ins := &body[i]
		switch ins.op {
		case opText:
			sb.WriteString(ins.text)
		case opOutput:
			v, err := evalExpr(prog.exprs[ins.exprIdx], sc)
			if err != nil {
				return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.exprIdx]), Msg: err.Error()}
			}
			s := stringify(v)
			if ins.escape {
				s = html.EscapeString(s)
			}
			sb.WriteString(s)
		case opIf:
			done := false
			for _, br := range ins.branches {
				if br.condIdx < 0 {
					if err := t.exec(prog, br.body, sc, sb); err != nil {
						return err
					}
					done = true
					break
				}
				v, err := evalExpr(prog.exprs[br.condIdx], sc)
				if err != nil {
					return &RenderError{Template: prog.name, Path: exprString(prog.exprs[br.condIdx]), Msg: err.Error()}
				}
				if truthy(v) {
					if err := t.exec(prog, br.body, sc, sb); err != nil {
						return err
					}
					done = true
					break
				}
			}
			_ = done
		case opFor:
			it, err := evalExpr(prog.exprs[ins.iterIdx], sc)
			if err != nil {
				return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.iterIdx]), Msg: err.Error()}
			}
			if err := t.execFor(prog, ins, it, sc, sb); err != nil {
				return err
			}
		case opCall:
			sub, ok := t.progs[ins.callName]
			if !ok {
				return &RenderError{Template: prog.name, Path: ins.callName, Msg: "referenced template not compiled"}
			}
			child := &scope{vars: map[string]any{}, parent: sc}
			// 稳定顺序求参，便于错误定位
			names := make([]string, 0, len(ins.callParams))
			for k := range ins.callParams {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				v, err := evalExpr(prog.exprs[ins.callParams[k]], sc)
				if err != nil {
					return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.callParams[k]]), Msg: err.Error()}
				}
				child.vars[k] = v
			}
			if err := t.exec(sub, sub.body, child, sb); err != nil {
				return err
			}
		}
	}
	return nil
}

// execFor 执行循环指令。
func (t *Template) execFor(prog *program, ins *instr, it any, sc *scope, sb *strings.Builder) error {
	rv := reflect.ValueOf(it)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.iterIdx]), Msg: "cannot iterate nil"}
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.iterIdx]), Msg: "cannot iterate nil"}
	}
	child := &scope{vars: map[string]any{}, parent: sc}
	set := func(k, v any) {
		if ins.keyVar != "" {
			child.vars[ins.keyVar] = k
		}
		child.vars[ins.valVar] = v
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			set(int64(i), rv.Index(i).Interface())
			if err := t.exec(prog, ins.body, child, sb); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		keys := rv.MapKeys()
		sort.Slice(keys, func(a, b int) bool {
			ka, kb := keys[a], keys[b]
			if fa, ok := toFloat(ka.Interface()); ok {
				if fb, ok := toFloat(kb.Interface()); ok {
					return fa < fb
				}
			}
			return fmt.Sprint(ka.Interface()) < fmt.Sprint(kb.Interface())
		})
		for _, k := range keys {
			set(k.Interface(), rv.MapIndex(k).Interface())
			if err := t.exec(prog, ins.body, child, sb); err != nil {
				return err
			}
		}
		return nil
	case reflect.String:
		for i, r := range rv.String() {
			set(int64(i), string(r))
			if err := t.exec(prog, ins.body, child, sb); err != nil {
				return err
			}
		}
		return nil
	}
	return &RenderError{Template: prog.name, Path: exprString(prog.exprs[ins.iterIdx]),
		Msg: fmt.Sprintf("cannot iterate %T", it)}
}

// stringify 将求值结果转为输出字符串。
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	}
	return fmt.Sprint(v)
}

// exprString 将表达式还原为源码形式，用于错误中的数据路径定位。
func exprString(e expr) string {
	switch n := e.(type) {
	case *litExpr:
		return fmt.Sprint(n.val)
	case *identExpr:
		return n.name
	case *fieldExpr:
		return exprString(n.obj) + "." + n.name
	case *indexExpr:
		return exprString(n.obj) + "[" + exprString(n.index) + "]"
	case *unaryExpr:
		return n.op + " " + exprString(n.x)
	case *binaryExpr:
		return exprString(n.left) + " " + n.op + " " + exprString(n.right)
	}
	return "?"
}
