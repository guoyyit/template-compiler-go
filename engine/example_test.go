package engine

import "fmt"

func ExampleEngine_Render() {
	e := NewEngine()
	e.AddTemplate("hello", `Hello, {{ name }}!`)
	out, err := e.Render("hello", map[string]any{"name": "world"})
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output: Hello, world!
}
