package engine

import (
	"fmt"
	"html"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// frame 是渲染时的数据作用域。
type frame struct {
	data map[string]any
}

// loopState 是循环执行状态。
type loopState struct {
	varName string
	items   []any
	index   int
}

// vm 是字节码执行器。
type vm struct {
	comp   *CompiledTemplate
	writer io.Writer
	engine *Engine
	frames []frame
	loops  []loopState
}

// execute 执行已编译模板。
func (v *vm) execute(data map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &RenderError{
				Template: v.comp.Name,
				Msg:      fmt.Sprintf("internal error: %v", r),
			}
		}
	}()
	if data == nil {
		data = map[string]any{}
	}
	v.frames = append(v.frames, frame{data: data})

	for pc := 0; pc < len(v.comp.Instr); pc++ {
		in := v.comp.Instr[pc]
		switch in.Op {
		case opText:
			if _, werr := io.WriteString(v.writer, in.Text); werr != nil {
				return &RenderError{Template: v.comp.Name, Msg: "write failed", Cause: werr}
			}
		case opWrite:
			val, perr := v.eval(in.Expr)
			if perr != nil {
				return perr
			}
			s := valueToString(val)
			if !in.Safe {
				s = html.EscapeString(s)
			}
			if _, werr := io.WriteString(v.writer, s); werr != nil {
				return &RenderError{Template: v.comp.Name, Msg: "write failed", Cause: werr}
			}
		case opJumpIfFalse:
			val, perr := v.eval(in.Expr)
			if perr != nil {
				return perr
			}
			if !isTruthy(val) {
				pc = in.Target - 1
			}
		case opJump:
			pc = in.Target - 1
		case opForInit:
			val, perr := v.eval(in.Expr)
			if perr != nil {
				return perr
			}
			items, lerr := toIterable(val)
			if lerr != nil {
				return lerr
			}
			v.loops = append(v.loops, loopState{varName: in.Var, items: items, index: 0})
			if len(items) == 0 {
				pc = in.Target - 1
			}
		case opForNext:
			top := &v.loops[len(v.loops)-1]
			top.index++
			if top.index >= len(top.items) {
				v.loops = v.loops[:len(v.loops)-1]
			} else {
				pc = in.Target - 1
			}
		case opInclude:
			if ierr := v.executeInclude(in); ierr != nil {
				return ierr
			}
		default:
			return &RenderError{Template: v.comp.Name, Msg: fmt.Sprintf("unknown opcode %d", in.Op)}
		}
	}
	return nil
}

// executeInclude 渲染被引用模板，参数在当前作用域求值。
func (v *vm) executeInclude(in Instr) error {
	data := map[string]any{}
	for _, p := range in.Params {
		val, err := v.eval(&p.Value)
		if err != nil {
			return err
		}
		data[p.Name] = val
	}
	if len(in.Params) == 0 {
		data = v.currentData()
	}
	partial, err := v.engine.Compile(in.Name)
	if err != nil {
		return &RenderError{
			Template: v.comp.Name,
			Msg:      fmt.Sprintf("include %q failed", in.Name),
			Cause:    err,
		}
	}
	sub := &vm{comp: partial, writer: v.writer, engine: v.engine}
	if err := sub.execute(data); err != nil {
		return &RenderError{
			Template: v.comp.Name,
			Msg:      fmt.Sprintf("include %q failed", in.Name),
			Cause:    err,
		}
	}
	return nil
}

// currentData 汇总当前所有帧与循环变量（无参数 include 时使用）。
func (v *vm) currentData() map[string]any {
	out := map[string]any{}
	for _, f := range v.frames {
		for k, val := range f.data {
			out[k] = val
		}
	}
	for _, l := range v.loops {
		out[l.varName] = l.items[l.index]
	}
	return out
}

// ---------- 表达式求值 ----------

func (v *vm) eval(e *Expr) (any, error) {
	switch e.Kind {
	case ExprLit:
		switch e.Tag {
		case litStr:
			return e.Str, nil
		case litInt:
			return e.Int, nil
		case litFloat:
			return e.Float, nil
		case litBool:
			return e.Bool, nil
		default:
			return nil, v.pathError(e, "invalid literal")
		}
	case ExprNil:
		return nil, nil
	case ExprPath:
		return v.evalPath(e)
	case ExprUnary:
		val, err := v.eval(&e.Args[0])
		if err != nil {
			return nil, err
		}
		switch e.Op {
		case "!":
			return !isTruthy(val), nil
		case "-":
			f, ok := toFloat(val)
			if !ok {
				return nil, v.pathError(e, fmt.Sprintf("operator - on non-numeric value %T", val))
			}
			return -f, nil
		default:
			return nil, v.pathError(e, fmt.Sprintf("unknown unary operator %q", e.Op))
		}
	case ExprBinary:
		return v.evalBinary(e)
	case ExprCall:
		return v.evalCall(e)
	default:
		return nil, &RenderError{Template: v.comp.Name, Msg: "invalid expression"}
	}
}

func (v *vm) evalBinary(e *Expr) (any, error) {
	op := e.Op
	// 短路求值
	if op == "&&" {
		l, err := v.eval(&e.Args[0])
		if err != nil {
			return nil, err
		}
		if !isTruthy(l) {
			return false, nil
		}
		r, err := v.eval(&e.Args[1])
		if err != nil {
			return nil, err
		}
		return isTruthy(r), nil
	}
	if op == "||" {
		l, err := v.eval(&e.Args[0])
		if err != nil {
			return nil, err
		}
		if isTruthy(l) {
			return true, nil
		}
		r, err := v.eval(&e.Args[1])
		if err != nil {
			return nil, err
		}
		return isTruthy(r), nil
	}

	l, err := v.eval(&e.Args[0])
	if err != nil {
		return nil, err
	}
	r, err := v.eval(&e.Args[1])
	if err != nil {
		return nil, err
	}
	switch op {
	case "==":
		return deepEqual(l, r), nil
	case "!=":
		return !deepEqual(l, r), nil
	case "<", ">", "<=", ">=":
		return compareValues(op, l, r)
	case "+", "-", "*", "/", "%":
		return arith(op, l, r)
	default:
		return nil, v.pathError(e, fmt.Sprintf("unknown operator %q", op))
	}
}

func (v *vm) evalCall(e *Expr) (any, error) {
	switch e.Func {
	case "len":
		if len(e.Args) != 1 {
			return nil, v.pathError(e, "len() expects 1 argument")
		}
		val, err := v.eval(&e.Args[0])
		if err != nil {
			return nil, err
		}
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
			return int64(rv.Len()), nil
		default:
			return nil, v.pathError(e, fmt.Sprintf("len() on unsupported type %T", val))
		}
	default:
		return nil, v.pathError(e, fmt.Sprintf("unknown function %q", e.Func))
	}
}

// evalPath 解析变量路径，如 a.b[0].c。
func (v *vm) evalPath(e *Expr) (any, error) {
	if len(e.Path) == 0 {
		return nil, v.pathError(e, "empty path")
	}
	first := e.Path[0]
	val, pathStr, err := v.lookupName(first.Name)
	if err != nil {
		return nil, &RenderError{Template: v.comp.Name, Path: pathStr, Msg: err.Error()}
	}
	for i := 1; i < len(e.Path); i++ {
		seg := e.Path[i]
		if seg.Name != "" {
			pathStr += "." + seg.Name
			if val == nil {
				return nil, nil
			}
			m, ok := val.(map[string]any)
			if !ok {
				return nil, &RenderError{
					Template: v.comp.Name, Path: pathStr,
					Msg: fmt.Sprintf("cannot access field %q on %T", seg.Name, val),
				}
			}
			var exists bool
			val, exists = m[seg.Name]
			if !exists {
				return nil, &RenderError{
					Template: v.comp.Name, Path: pathStr,
					Msg:      fmt.Sprintf("field %q not found", seg.Name),
				}
			}
		} else {
			idx, ierr := v.eval(seg.Index)
			if ierr != nil {
				return nil, ierr
			}
			pathStr += fmt.Sprintf("[%v]", idx)
			val, err = indexValue(val, idx)
			if err != nil {
				return nil, &RenderError{Template: v.comp.Name, Path: pathStr, Msg: err.Error()}
			}
		}
	}
	return val, nil
}

// lookupName 依次查找循环变量与数据帧。
func (v *vm) lookupName(name string) (any, string, error) {
	for i := len(v.loops) - 1; i >= 0; i-- {
		if v.loops[i].varName == name {
			return v.loops[i].items[v.loops[i].index], name, nil
		}
	}
	for i := len(v.frames) - 1; i >= 0; i-- {
		if val, ok := v.frames[i].data[name]; ok {
			return val, name, nil
		}
	}
	// 未定义变量按 nil 处理（宽松语义）
	return nil, name, nil
}

func (v *vm) pathError(e *Expr, msg string) error {
	path := ""
	if e.Kind == ExprPath {
		path = pathToString(e.Path)
	}
	return &RenderError{Template: v.comp.Name, Path: path, Msg: msg}
}

func pathToString(segs []PathSeg) string {
	var sb strings.Builder
	for i, s := range segs {
		if s.Name != "" {
			if i > 0 {
				sb.WriteByte('.')
			}
			sb.WriteString(s.Name)
		} else {
			sb.WriteString("[]")
		}
	}
	return sb.String()
}

// ---------- 值工具 ----------

func valueToString(val any) string {
	if val == nil {
		return ""
	}
	switch x := val.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func isTruthy(val any) bool {
	if val == nil {
		return false
	}
	switch x := val.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int64:
		return x != 0
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			return rv.Len() > 0
		}
		return true
	}
}

func toFloat(val any) (float64, bool) {
	switch x := val.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}

func deepEqual(a, b any) bool {
	fa, aok := toFloat(a)
	fb, bok := toFloat(b)
	if aok && bok {
		return fa == fb
	}
	return reflect.DeepEqual(a, b)
}

func compareValues(op string, a, b any) (any, error) {
	fa, aok := toFloat(a)
	fb, bok := toFloat(b)
	if aok && bok {
		switch op {
		case "<":
			return fa < fb, nil
		case ">":
			return fa > fb, nil
		case "<=":
			return fa <= fb, nil
		case ">=":
			return fa >= fb, nil
		}
	}
	sa, aok := a.(string)
	sb, bok := b.(string)
	if aok && bok {
		switch op {
		case "<":
			return sa < sb, nil
		case ">":
			return sa > sb, nil
		case "<=":
			return sa <= sb, nil
		case ">=":
			return sa >= sb, nil
		}
	}
	return nil, fmt.Errorf("cannot compare %T and %T", a, b)
}

func arith(op string, a, b any) (any, error) {
	if op == "+" {
		sa, aok := a.(string)
		sb, bok := b.(string)
		if aok && bok {
			return sa + sb, nil
		}
	}
	fa, aok := toFloat(a)
	fb, bok := toFloat(b)
	if !aok || !bok {
		return nil, fmt.Errorf("arithmetic %q requires numbers, got %T and %T", op, a, b)
	}
	switch op {
	case "+":
		return fa + fb, nil
	case "-":
		return fa - fb, nil
	case "*":
		return fa * fb, nil
	case "/":
		if fb == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return fa / fb, nil
	case "%":
		if fb == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return float64(int64(fa) % int64(fb)), nil
	}
	return nil, fmt.Errorf("unknown operator %q", op)
}

func toIterable(val any) ([]any, error) {
	if val == nil {
		return nil, fmt.Errorf("cannot iterate over nil")
	}
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	default:
		return nil, fmt.Errorf("cannot iterate over %T", val)
	}
}

func indexValue(val any, idx any) (any, error) {
	if val == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		f, ok := toFloat(idx)
		if !ok {
			return nil, fmt.Errorf("slice index must be a number, got %T", idx)
		}
		i := int(f)
		if i < 0 || i >= rv.Len() {
			return nil, fmt.Errorf("index %d out of range (len %d)", i, rv.Len())
		}
		return rv.Index(i).Interface(), nil
	case reflect.Map:
		key := reflect.ValueOf(idx)
		if key.Type().AssignableTo(rv.Type().Key()) {
			v := rv.MapIndex(key)
			if !v.IsValid() {
				return nil, fmt.Errorf("key %v not found", idx)
			}
			return v.Interface(), nil
		}
		// 尝试字符串键
		if rv.Type().Key().Kind() == reflect.String {
			s := valueToString(idx)
			v := rv.MapIndex(reflect.ValueOf(s))
			if !v.IsValid() {
				return nil, fmt.Errorf("key %q not found", s)
			}
			return v.Interface(), nil
		}
		return nil, fmt.Errorf("cannot index map with key %T", idx)
	default:
		return nil, fmt.Errorf("cannot index %T", val)
	}
}
