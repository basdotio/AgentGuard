<!-- 常驻规则:没有 paths,会话开始即加载(P-003) -->
## 约定

- 每个 `.go` 文件开头都是 `// SPDX-License-Identifier: MIT`;每个包挑一个文件写包级 doc 注释,
  并在其中声明该包负责的不变量。
- **代码和所有用户可见字符串只用英文。** 文档做双语对子(`README.md`/`README.zh-CN.md`、
  `docs/llm-judge.md`/`docs/llm-judge.zh-CN.md`、`docs/architecture.md`/`docs/architecture.zh-CN.md`)
  —— 改一个就要改另一个。**两个例外:**
  - `docs/rules.md`:它的正文就是代码里的字符串(按上一条必须是英文),而手工翻译的副本无法被
    CI 的漂移检查证明为真 —— 一份没人能验证的规则表比没有更糟。
  - **`docs/corpus-benchmark.zh-CN.md` 和 `docs/spec/spec.zh-CN.md` 是中文单语,不做双语对子。** 它们是维护者
    文档,改动频繁,做成对子等于把维护量翻倍,而且两份会各自漂移。要英文的读者先靠 `docs/architecture.md`。
- **发布产物只有 darwin/linux**(`Makefile` 的 `DIST_TARGETS`)。Windows 能干净交叉编译,而这正是
  陷阱:CI 只跑 ubuntu、代码里没有任何 `GOOS` 分支、不变量 #2 的符号链接约束依赖的原语在那边行为
  不同(建符号链接要特权,边界用例根本跑不到)。**要加回 windows,先加能在上面跑完整套测试的 CI
  matrix**,别只改 Makefile。
- 注释会引用规格(`spec §16.3`)和历次评审结论(`review B-1`、`F2`、`A1`、`M5.2`)。请延续这个风格:
  一处不显然的防护,应当说明它实现的是哪条要求。
- 纯 Go、不用 CGO,保持单静态二进制。直接依赖三个:`spf13/cobra`、`gopkg.in/yaml.v3`、
  `golang.org/x/term`(给 `clean --ask` 的方向键做 raw mode)。
  **`x/term` 的版本是钉住的,不要随手升**:`golang.org/x/term v0.28.0` + `golang.org/x/sys v0.29.0`
  (indirect,v0.29.0 是 term v0.28.0 自己要求的最低值)。
  实测 `go get golang.org/x/term`(不带版本)拿到 v0.45.0,它要求 `go >= 1.25.0`,`go mod tidy`
  就把本仓库的 go 指令从 **1.23.5 顶到 1.25.0** —— 而 CI 两个 workflow 都钉 `go-version: '1.23'`,
  README 徽章写 `go-1.23+`。**一次 push 就红,而且不是测试失败,是编译不了。**
  **降级要用 `go mod edit -require`,`go get <低版本>` 不管用**(MVS 只往高走,而且不会告诉你没降下来):

  ```bash
  go mod edit -go=1.23.5 -require=golang.org/x/term@v0.28.0 -require=golang.org/x/sys@v0.29.0
  go mod tidy && head -3 go.mod   # 第二行必须还是 go 1.23.5
  ```

  **验收看 `go version` 有没有自动切换工具链**:出现 `switching to go1.25.x` 就说明还有一条
  `>= 1.25` 的要求没钉住,而 `make test` 在切换后的工具链上照样全绿 —— 绿灯在这里不构成证据。
  **再加第四个依赖前先照这个标准论证一遍,并且必须看它的 `go` 指令。**
- **终端相关的代码必须可测,而且不能是承重的。** `internal/clean/termraw.go` 是唯一碰真实终端的
  文件(约 15 行,两个入口是**包级变量**,测试直接替换);按键解析在
  [rawline.go](../../internal/clean/rawline.go),吃 `io.Reader`,用字节 fixture 测。
  **更关键的是分层**:按键层只负责把按键变成"人本来也能敲出来的那个字符串",
  它**改不了答案导致什么**——旧版把终端解码放在一切之下,一个拆错的转义就变成了一次写盘。
- **终端/交互代码的测试,必须把"没人回答"和"回答了别的"当成两种输入分别测。**
  撤回的那个选择器测试是绿的,而它:用一条 table 用例**给缺陷发了合格证**(断言"未知转义固定吞
  3 字节",那正是 `Shift+Down` 被拆成 keep-both 的原因);fixture 在结构上造不出多于一对的场景,
  于是"没有一个按键能一次回答多对"这条头号承诺一次都没被断言;fallback 那条路一行测试都没有。
  **只测"回答了正确的"等于没测。**
- 测试是 table-driven 的,fixture 用 `t.TempDir()` 现搭(没有 `testdata/` 目录);judge 的测试用
  `httptest`。真正值钱的是**不变量测试** —— 不执行、符号链接不越界、secret 已脱敏、LLM 发现不动分数、
  `--fail-on` 契约。上面编号的不变量只要有改动,就补一条对应的测试。
- 推迟/待办的事项记在 [ROADMAP.md](../../ROADMAP.md),确认存在的缺陷与否决记录在
  [issues/](../../issues/README.md) —— 分工见 CLAUDE.md 开头那张表。
