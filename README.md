# template-compiler-go

把模板文本**编译**成可复用的渲染产物，再由运行时高效执行的 Go 模板引擎。
完整链路：解析 → 编译（含引用图检查）→ 缓存/序列化 → 并发渲染。

```go
src := template.MapSource{
    "main": `Hello {{ user.name }}! {% include "bio" with v=user.bio %}`,
    "bio":  `bio={{{ v }}}`,
}
tpl, err := template.Compile("main", src)   // 编译一次
out, err := tpl.Render(map[string]any{       // 渲染任意多次
    "user": map[string]any{"name": "<Bob>", "bio": "<b>hi</b>"},
})
// out = "Hello &lt;Bob&gt;! bio=<b>hi</b>"
```

## 模板语法

| 语法 | 说明 |
| --- | --- |
| `{{ expr }}` | 插值输出，**默认按 HTML 规则转义** |
| `{{{ expr }}}` | 插值输出，**明确跳过转义**（raw） |
| `{% if expr %} ... {% elif expr %} ... {% else %} ... {% endif %}` | 条件分支 |
| `{% for x in expr %} ... {% endfor %}` | 循环（slice/array/map/string） |
| `{% for k, v in expr %} ... {% endfor %}` | 带键/下标的循环；map 按键排序遍历，保证输出确定 |
| `{% include "name" %}` | 引用局部模板 |
| `{% include "name" with a=expr, b=expr %}` | 引用并传参；参数在被子模板中覆盖同名变量，调用方作用域沿链可见 |

表达式支持：数值/字符串/布尔/nil 字面量、变量、`a.b` 字段、`a[i]` 下标、
`+ - * / %`、`== != < <= > >=`、`and or not`（含 `&& || !`，逻辑运算短路）、括号。

## 编译期错误（不会推迟到渲染）

所有语法与引用问题都在 `Compile`/`Cache.Get` 阶段返回，类型可区分：

- `*SyntaxError`：语法错误，携带模板名与**行列号**（`Pos.Line/Col`）。
- `*CycleError`：循环引用，`Path` 为完整引用链，如 `a -> b -> c -> a`。
- `*MissingError`：引用缺失，`Path` 为引用链、`Missing` 为缺失模板名。

## 转义规则

- `{{ ... }}` 的输出经 `html.EscapeString` 转义；`{{{ ... }}}` 原样输出。
- 转义决策在**编译期**固化进产物指令（`escape` 标志），因此：
  - 缓存命中与未命中路径行为完全一致（有测试 `TestCacheHitAndMissIdentical` 保证）；
  - 序列化/反序列化后的产物行为完全一致（`TestMarshalRoundTripIdenticalOutput`）。

## 缓存语义

`Cache` 并发安全（`sync.RWMutex` + 按名单次编译合并）：

- 缓存键 = 模板名 + 源内容 SHA-256 指纹。**源变化后自动重新编译**，无需手动失效；
  也可 `Invalidate(name)` 强制失效。
- 条目写满后才原子发布，读者永远不会读到半成品；并发渲染 + 并发重编译无数据竞争
  （`TestConcurrentRenderAndRecompile`，`-race` 通过）。
- 同名并发未命中只编译一次，其余等待者共享结果。

## 序列化产物

```go
blob, _ := tpl.Marshal()          // 可落盘/跨进程传输
tpl2, err := template.Unmarshal(blob)
```

- 产物带 `magic` 与 `FormatVersion`（当前为 1）。
- 版本不兼容或数据损坏时返回 `*VersionError`；`Cache.Load(name, src, blob)`
  会在此时**安全回退到重新编译**，不会崩溃或静默产出错误结果。

## 运行期错误隔离

渲染失败返回 `*RenderError`，携带模板标识（`Template`）与引发问题的数据路径
（`Path`，如 `user.bio`）。渲染状态每次调用独立，单个模板失败不污染其他模板或缓存。

## 性能

重复渲染不重新解析/编译，开销随输出规模线性增长、与模板文本规模无关的固定开销趋近于零。
基准（Xeon 8255C, go1.26，`go test -bench=. -benchmem`）：

| 基准 | 结果 |
| --- | --- |
| 编译（100 块大模板） | ~19 µs/次，仅发生一次 |
| 渲染 1 项 | ~3.4 µs |
| 渲染 1000 项 | ~2.3 ms（每项约 2.3 µs，线性） |
| 并发渲染（100 项） | ~91 µs/op |
| 缓存命中 `Get` | ~1.8 µs |

复现：`go test ./template/ -bench=. -benchmem -run=NONE`

## 测试

```sh
go test ./... -race          # 语法错误/循环引用/转义一致性/缓存失效/并发/产物兼容回退
go test ./template/ -bench=. -benchmem -run=NONE
```
