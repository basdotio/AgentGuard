<!-- SPDX-License-Identifier: MIT -->
# 003 — "绝不外连"没有一条测试钉住,baselines 的模板却说有

- **来源**:新发现(2026-10-09)—— 不变量 #1(默认零外连)是工具可信的根据,但没有任何测试钉住它;
  baselines 的说明文字却声称由测试保证。移植自旧仓 agent-guard 的 P-044(私有仓)
- **依赖**:无
- **分支**:`p/003-zero-dial-test`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

不变量 #1(`.claude/rules/invariants.md:7`)写着"绝不执行被扫描内容,绝不外连(除显式开启的 LLM judge)"。
README、`docs/architecture.md` 的简表、`cmd/aguard` 里几处注释都在复述它。**可是全仓没有一条测试断言过它**:

| 地方 | 现在写着什么 | 实际有什么 |
|---|---|---|
| `baselines/tools.yaml:33-37`(aguard 的 `uploads_samples_basis`) | "invariant #1, **enforced by tests in this repository**: aguard never connects out except for the explicitly opted-in LLM judge" | `grep -rn 'RoundTrip\|DefaultTransport' --include='*_test.go'` 只命中判官自己的单测名(`TestHTTPClient_RoundTripAndRedaction`)和 baselines 的一个 YAML 往返测试;判官的 `httptest` 单测测的是"判官发了什么",不是"别的命令没发" |
| `baselines/cmd/baseline/main.go:397-413` `uploadsFor` | 不带 `--llm` 时把上面那句原样写进每一份 run.yaml | 已提交的 `baselines/results/aguard/2026-09-24/run.yaml:10`、`2026-09-29e/run.yaml:10` 都带着这句 |
| `cmd/aguard/llm.go:112` `runLLMSetup` 的注释 | "It never prints the key and never sends anything" | 没有测试 |
| `cmd/aguard/main.go:807` `version` 命令的注释 | "Offline by construction … never a release feed" | 没有测试 |

今天这句话**碰巧是真的**:产品里唯一的 `http.Client` 在 `internal/judge/openai.go:52-65` 的 `NewHTTP`
(传 `nil` 就新建一个 `&http.Client{}`),两个调用点 `cmd/aguard/main.go:273`(`runJudge`)和 `cmd/aguard/llm.go:220`
(`runLLMTest`)都传 `nil`;信誉库是 `go:embed`,`gate`、`collect` 没有网络代码,`hack/reputation-refresh` 是另一个二进制。
但"碰巧是真的"和"被测试钉住"是两回事:

- **下一次加网络调用的人没有任何东西会变红。** 一个顺手的"setup 时验证一下 key"、"version 时查一下新版本"、
  "check 也跑判官"(P-004 做的正是最后这一条),都会把一条新的出网路径加进来而全绿。
- **对外引用的那份文件在替我们说一句没有证据的话。** run.yaml 是别人引用测量结果时读的文件(`uploadsFor` 的注释原话),
  它说"enforced by tests",而那些测试不存在。本仓库的披露纪律是"绿灯不构成证据";这里连绿灯都没有。
- **不变量本身也说不清边界。** "除显式开启的 LLM judge"没说是哪几个命令。`scan --llm` 会连,`llm test` 也会连
  (一次 Ping),`check`、`hook`、`approve` 不会 —— 这份清单只在读代码的人脑子里。

## 初步方向

`internal/judge` 加一个包级测试接缝 `var Transport http.RoundTripper`(nil = `http.DefaultTransport`,和今天一样),
`NewHTTP` 在调用方没给 client 时用它。`cmd/aguard` 加一条进程内测试:配置里**开着**判官,把接缝换成只计数、返回错误的
RoundTripper,逐个跑命令入口,断言零次 round trip;同一个计数器在 `scan --llm` 上必须看到 ≥ 1 次,证明它不是瞎的。

不变量 #1 改写成"今天只有这几条路径可以出网"的枚举清单,并写明钉住它的是哪条测试;baselines 那句改成点名这条测试。
不改判官行为,不加网络路径,已提交的 `baselines/results/` 不动。
