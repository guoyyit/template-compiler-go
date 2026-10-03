package template_test

import (
	"fmt"

	template "github.com/guoyyit/template-compiler-go/template"
)

func Example() {
	src := template.MapSource{
		"main": `{% for u in users %}{{ u.name }}{% if u.admin %}*{% endif %} {% endfor %}`,
	}
	tpl, err := template.Compile("main", src)
	if err != nil {
		panic(err)
	}
	out, err := tpl.Render(map[string]any{
		"users": []any{
			map[string]any{"name": "ann", "admin": true},
			map[string]any{"name": "bob"},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output: ann* bob
}
