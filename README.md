# template-compiler-go

一套可编译的模板引擎：把模板文本编译成可复用的字节码产物，由运行时高效执行渲染。
支持变量插值、条件分支、循环、局部模板引用与组合、HTML 自动转义、内容寻址编译缓存、
产物序列化与跨进程复用。

## 快速开始

```go
e := engine.NewEngine()
e.AddTemplate("hello", `Hello, {{ name }}!`)
out, err := e.Render("hello", map[string]any{"name": "world"})
// out = "Hello, world!"
```

## 模板语法

### 变量插值

```
{{ expr }}    插值，结果按 HTML 规则转义
{{{ expr }}}  插值，原始输出（不转义）
```

### 条件分支

```
{% if user.active %}
  欢迎，{{ user.name }}！
{% else %}
  请先登录。
{% end %}
```

### 循环

```
{% for item in items %}
  {{ item.name }}（{{ item.count }}）
{% end %}
```

循环变量在循环体内遮蔽外层同名变量；支持任意 slice/array。

### 局部模板引用

```
{% include "header" %}
{% include "card" title=post.title, show=true %}
```

- 引用名必须是字符串字面量。
- 可携带具名参数，参数值在当前作用域求值；被引用模板获得独立数据作用域。
- 不传递参数时，被引用模板继承当前数据上下文（含循环变量）。
- 模板之间的引用关系构成有向图。**循环引用**与**引用缺失**在**编译阶段**即被拒绝，
  错误以可区分的类型给出完整引用链路，例如：

  ```
  compile error in a (chain a -> b -> c -> a): circular reference
  ```

### 表达式

- 字面量：字符串 `"..."`、整数、浮点、`true`、`false`、`nil`
- 变量路径：`user.address.city`、`items[0].name`、`m["key"]`
- 运算符：`+ - * / %`、`== != < > <= >=`、`&& || !`（也可写作 `and or not`）
- 函数：`len(x)` 返回字符串/slice/array/map 的长度
- 括号：`(a + b) * c`

### 注释

```
{# 这是注释，不会出现在输出中 #}
```

## 转义规则

- 插值结果**默认**按 HTML 规则转义（`html.EscapeString`）：`<`、`>`、`&`、`"`、`'`
  分别转义为 `&lt;`、`&gt;`、`&amp;`、`&#34;`、`&#39;`。
- 使用 `{{{ ... }}}` 明确标注原始输出，跳过转义。
- 同一份模板无论从编译缓存命中还是未命中路径渲染，转义行为完全一致；
  缓存只保存编译产物，不保存渲染结果，不会因缓存复用改变语义。

## 缓存语义

- 缓存键由**模板内容的 SHA-256** 决定（内容寻址）。内容不变时重复编译返回同一份产物；
  源码变化后内容哈希变化，自动重新编译，旧缓存项自然失效。
- 相同内容的不同模板名共享同一份编译产物。
- 并发编译同一内容通过 singleflight 去重：只有一个编译 goroutine 工作，其余等待结果。
- 编译产物一经发布不可变；并发渲染与并发重新编译同时进行时，
  渲染要么读到旧产物、要么读到新产物，**不会读到半成品**，也不存在数据竞争
  （`Store` 与 `compileCache` 均为并发安全设计，已通过 `-race` 验证）。

## 产物序列化

编译产物可序列化为字节流，在进程间传输或落盘后复用：

```go
var buf bytes.Buffer
e.SaveArtifact("page", &buf)   // 序列化

// 另一个进程/实例
name, err := e2.LoadArtifact(&buf)
```

文件格式：

```
magic "TCG1" (4B) | version (1B) | payloadSHA256 (32B) | gob payload
```

- 魔数或版本不兼容 → `ErrArtifactIncompatible`
- 校验和失败或数据损坏 → `ErrArtifactCorrupt`
- 产物与当前源码哈希不匹配 → 拒绝复用

读取到不兼容或损坏的产物时，调用方应回退到重新编译：

```go
if _, err := e.LoadArtifact(r); err != nil {
    // 回退：重新编译并渲染
    e.Render(name, data)
}
```

产物加载后会做健全性校验（操作码合法性、跳转目标范围、表达式非空），
损坏数据不会导致渲染崩溃。

## 错误模型

| 错误类型 | 触发阶段 | 携带信息 |
| --- | --- | --- |
| `SyntaxError` | 编译 | 文件名、行:列、语法说明 |
| `CompileError` | 编译 | 模板名、完整引用链路、原因（缺失/循环） |
| `RenderError` | 渲染 | 模板标识、数据路径、原因 |

渲染期错误（如除零、对非 map 取字段）携带模板名与数据路径，例如：

```
render error in template "page" at user.address.city: field "city" not found
```

单个模板渲染失败不会污染缓存或其他模板：渲染在独立 VM 实例中进行，
错误以返回值形式给出，缓存中只存放编译产物。

## 性能特征

- 模板**只编译一次**，重复渲染直接执行字节码，无需重新解析。
- 重复渲染开销主要与输出规模相关；与编译相比，渲染省去了词法/语法分析与引用图校验开销。
- 高并发渲染通过 `sync.RWMutex` 读路径与不可变产物保证稳定。

运行基准测试：

```bash
go test -bench Benchmark -benchtime=100x ./engine
```

参考数据（`BenchmarkRenderSmall` / `BenchmarkRenderLarge` / `BenchmarkCompile`，
1000 个插值点的模板）：

- 小模板重复渲染：约 1 µs/次
- 大模板（1000 插值点）重复渲染：约 0.6 ms/次
- 同规模冷编译：约 5.4 ms/次（约为渲染开销的 9 倍）
- 并发渲染（4 核）：约 26 µs/次（100 插值点）

## 测试

```bash
go test ./... -race
```

测试覆盖：语法错误定位、循环引用、缺失引用、菱形依赖、转义一致性（缓存命中 vs 未命中）、
缓存失效、并发渲染与并发重编译、产物序列化/损坏/不兼容回退、渲染错误隔离等。
