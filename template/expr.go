package template

import (
	"fmt"
	"reflect"
)

// expr 表达式语法树节点。
type expr interface{ exprPos() Pos }

type litExpr struct {
	pos Pos
	val any
}

func (e *litExpr) exprPos() Pos { return e.pos }

type identExpr struct {
	pos  Pos
	name string
}

func (e *identExpr) exprPos() Pos { return e.pos }

type fieldExpr struct {
	pos  Pos
	obj  expr
	name string
}

func (e *fieldExpr) exprPos() Pos { return e.pos }

type indexExpr struct {
	pos   Pos
	obj   expr
	index expr
}

func (e *indexExpr) exprPos() Pos { return e.pos }

type unaryExpr struct {
	pos Pos
	op  string
	x   expr
}

func (e *unaryExpr) exprPos() Pos { return e.pos }

type binaryExpr struct {
	pos   Pos
	op    string
	left  expr
	right expr
}

func (e *binaryExpr) exprPos() Pos { return e.pos }

// truthy 判断值的真值。
func truthy(v any) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b != ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return !rv.IsZero()
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
		return rv.Len() != 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	}
	return true
}

// toFloat 将数值转为 float64。
func toFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// lookupField 在 map/struct 上按名字取值。
func lookupField(obj any, name string) (any, error) {
	if obj == nil {
		return nil, fmt.Errorf("cannot access field %q of nil", name)
	}
	rv := reflect.ValueOf(obj)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, fmt.Errorf("cannot access field %q of nil", name)
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		mv := rv.MapIndex(reflect.ValueOf(name))
		if !mv.IsValid() {
			return nil, nil // 缺失键按 nil 处理
		}
		return mv.Interface(), nil
	case reflect.Struct:
		fv := rv.FieldByName(name)
		if fv.IsValid() && fv.CanInterface() {
			return fv.Interface(), nil
		}
		return nil, fmt.Errorf("no field %q on %s", name, rv.Type())
	}
	return nil, fmt.Errorf("cannot access field %q of %T", name, obj)
}

// lookupIndex 在 slice/array/map 上按下标取值。
func lookupIndex(obj, idx any) (any, error) {
	rv := reflect.ValueOf(obj)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, fmt.Errorf("cannot index nil")
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.String:
		f, ok := toFloat(idx)
		if !ok {
			return nil, fmt.Errorf("index must be a number, got %v", idx)
		}
		i := int(f)
		if i < 0 || i >= rv.Len() {
			return nil, fmt.Errorf("index %d out of range (len %d)", i, rv.Len())
		}
		return rv.Index(i).Interface(), nil
	case reflect.Map:
		mv := rv.MapIndex(reflect.ValueOf(idx))
		if !mv.IsValid() {
			return nil, nil
		}
		return mv.Interface(), nil
	}
	return nil, fmt.Errorf("cannot index %T", obj)
}

// evalExpr 在给定作用域上求值。
func evalExpr(e expr, sc *scope) (any, error) {
	switch n := e.(type) {
	case *litExpr:
		return n.val, nil
	case *identExpr:
		v, ok := sc.get(n.name)
		if !ok {
			return nil, fmt.Errorf("undefined variable %q", n.name)
		}
		return v, nil
	case *fieldExpr:
		obj, err := evalExpr(n.obj, sc)
		if err != nil {
			return nil, err
		}
		return lookupField(obj, n.name)
	case *indexExpr:
		obj, err := evalExpr(n.obj, sc)
		if err != nil {
			return nil, err
		}
		idx, err := evalExpr(n.index, sc)
		if err != nil {
			return nil, err
		}
		return lookupIndex(obj, idx)
	case *unaryExpr:
		x, err := evalExpr(n.x, sc)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "not":
			return !truthy(x), nil
		case "-":
			f, ok := toFloat(x)
			if !ok {
				return nil, fmt.Errorf("cannot negate %v", x)
			}
			return -f, nil
		}
		return nil, fmt.Errorf("unknown unary operator %q", n.op)
	case *binaryExpr:
		return evalBinary(n, sc)
	}
	return nil, fmt.Errorf("unknown expression node %T", e)
}

func evalBinary(n *binaryExpr, sc *scope) (any, error) {
	// 短路逻辑
	if n.op == "and" {
		l, err := evalExpr(n.left, sc)
		if err != nil {
			return nil, err
		}
		if !truthy(l) {
			return false, nil
		}
		r, err := evalExpr(n.right, sc)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	}
	if n.op == "or" {
		l, err := evalExpr(n.left, sc)
		if err != nil {
			return nil, err
		}
		if truthy(l) {
			return true, nil
		}
		r, err := evalExpr(n.right, sc)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	}
	l, err := evalExpr(n.left, sc)
	if err != nil {
		return nil, err
	}
	r, err := evalExpr(n.right, sc)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return valuesEqual(l, r), nil
	case "!=":
		return !valuesEqual(l, r), nil
	case "<", "<=", ">", ">=":
		lf, lok := toFloat(l)
		rf, rok := toFloat(r)
		if lok && rok {
			switch n.op {
			case "<":
				return lf < rf, nil
			case "<=":
				return lf <= rf, nil
			case ">":
				return lf > rf, nil
			case ">=":
				return lf >= rf, nil
			}
		}
		ls, lok := l.(string)
		rs, rok := r.(string)
		if lok && rok {
			switch n.op {
			case "<":
				return ls < rs, nil
			case "<=":
				return ls <= rs, nil
			case ">":
				return ls > rs, nil
			case ">=":
				return ls >= rs, nil
			}
		}
		return nil, fmt.Errorf("cannot compare %v and %v", l, r)
	case "+":
		if ls, ok := l.(string); ok {
			if rs, ok := r.(string); ok {
				return ls + rs, nil
			}
		}
		return arith(n.op, l, r)
	case "-", "*", "/", "%":
		return arith(n.op, l, r)
	}
	return nil, fmt.Errorf("unknown operator %q", n.op)
}

func arith(op string, l, r any) (any, error) {
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		return nil, fmt.Errorf("operator %q requires numbers, got %v and %v", op, l, r)
	}
	switch op {
	case "+":
		return lf + rf, nil
	case "-":
		return lf - rf, nil
	case "*":
		return lf * rf, nil
	case "/":
		if rf == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return lf / rf, nil
	case "%":
		if rf == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return float64(int64(lf) % int64(rf)), nil
	}
	return nil, fmt.Errorf("unknown operator %q", op)
}

// valuesEqual 宽松相等：数值互转比较，其余用 reflect.DeepEqual。
func valuesEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	return reflect.DeepEqual(a, b)
}
