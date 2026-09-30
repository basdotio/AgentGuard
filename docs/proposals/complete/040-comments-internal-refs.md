<!-- SPDX-License-Identifier: MIT -->
# 040 — 源码注释里 65 处 P-NNN、46 处 W-NNN、12 处 issues 路径和 3 处进了用户字符串的 spec 引用,新仓没有它们指向的东西

- **来源**:开源前清理扫描(2026-09-30)
- **依赖**:无(与 P-041 并行:那条搬文档,本条只动 Go 源码注释)
- **分支**:`p/040-comments-internal-refs`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

新仓没有 git 历史,也不带 `docs/proposals/`、`docs/planning/`、`docs/decisions/`、`docs/process.md`。
Go 源码注释却到处在引用它们:

| 什么 | 数量(`*.go`,不含另一分支要删的三个测试文件) | 读者拿到的是什么 |
|---|---|---|
| `(P-NNN)` / `P-NNN` 提案编号 | 112 处(cmd/internal/hack 58 处,baselines 54 处) | 一个指向不存在目录的编号;`git log --grep` 也没有历史可查 |
| `W-NNN` 工作项编号 | 46 处 | 同上,指向 `docs/planning/work-items.zh-CN.md` |
| `issues/NNN` 路径 | 12 处(clean、report、detect/owasp) | `issues/` 会留,但注释把"缺陷教了什么"外包给了那个文件,注释本身只剩一个指针 |
| 用户可见字符串里的 `spec §16.2` / `spec §5.2.1` | 3 处(HOOK-002 的 Why、LLM-005 的 Why) | 报告读者没有规格,一句话里的括注等于噪音 |
| 第三方样本集/厂商仓库名 | 7 处(`Trail of Bits, overtly-malicious-skills`、`dev-env-setup`、`Cisco's shape`) | 把测试集名字当解释,读者需要的是样本的**形状** |
| 规划黑话 | `decision 3 — not ported`、`deferred to P0.5; the P0 stand-in`、`ROADMAP B.7`、`Option A of the proposal`、一句引用的中文规划原文、`"Top 3 建议"` | 只有当时在场的人懂 |
| 非测试注释里的日期 | `since 2026-09-04`、`Measured (W-006, 2026-09-08)`、`Decided 2026-09-21` 等 | 在没有历史的仓里日期不锚定任何东西 |
| 版本沿革式长注释 | `unowned.go`、`gate.go`、`desktop.go`、`skip.go`、`collect.go`、`logical.go`、`manifest.go`、`usage.go`、`reputation-refresh`、`.golangci.yml`、`ci.yml` | "谁在哪条提案里何时决定"那一层盖住了真正值钱的"为什么这个防护存在" |

这些注释承载着本仓库的设计理由,评审看重它们。问题不是注释多,是**引用层**在新仓里全部落空。

## 初步方向

只动注释和上表那 3 条字符串。编号删掉、事实留下("measured on the corpus: 137 of 186 …" 这类数字照留);
`issues/NNN` 指针换成缺陷教了什么的一两句;厂商仓库名换成样本形状的描述;长注释压到一半左右,
只删"谁决定、何时、在哪条提案"和重复,不删任何一条解释防护为什么存在的理由。
`spec §N`(规格留在仓里)和 `invariant #N` 的引用**不动**;`Invariant Labs, 2025` 这类研究引用不动。

## 完成的判据

- [x] `grep -rn -E "\(P-0[0-9]{2}\)|\bP-0[0-9]{2}\b|\bW-0[0-9]{2}[a-z]?\b" --include='*.go' . | grep -v -E "cmd/aguard/(hooks_test|process_skills_test|claude_rules_test)\.go"`
  在**注释**里为 0。剩 4 处全在字符串字面量里(见未决问题 1),不在本条授权范围内。
- [x] `grep -rn -E "issues/[0-9]" --include='*.go' .` 为 0。
- [x] `internal/detect/hooks.go` 的两条 HOOK-002 Why 和 `internal/judge/run.go` 的 LLM-005 Why 不再含 `spec §`;
  `grep -rn 'spec §' --include='*.go' . | grep -v '//'` 只剩两条测试失败消息(`revshell_test.go:48`、`markdown_test.go:101`)。
- [x] 反向断言:行为一个字节没变。`git diff origin/dev..HEAD -- '*.go'` 里所有非注释改动只有:上面 3 条 Why 字符串、
  两条 `t.Error*` 失败消息里的 `issues/015` 指针、两个子测试的名字(`W-027 minimal repro`、`Cisco's shape`)、
  一行行尾注释的位置。没有增删改任何标识符,没有改任何测试断言;`gofmt -l .` 为空;`make docs` 不改 `docs/rules.md`。
- [x] `go build ./... && go vet ./... && go test ./...` 绿;`make verify` 绿。

## 不做什么

- 不动任何 `*.md`、`Makefile`、`.claude/`、`docs/`(本文件和索引一行除外)、`issues/`、`baselines/results/`、
  `hack/commit-msg`、`hack/check-proposal`、`hack/pre-commit`。
- 不动 `cmd/aguard/hooks_test.go`、`cmd/aguard/process_skills_test.go`、`cmd/aguard/claude_rules_test.go`(另一分支删除)。
- 不改上表 3 条之外的任何字符串,包括 `hack/gen-rules/main.go:107` LLM-009 描述里的 `(P-019)`(会漂进 `docs/rules.md`)
  和 `baselines/cmd/baseline/main.go` 三条驱动消息里的 `P-017` / `P-022 open question 5` / `P-021`。
- 不动 `spec §N`、`invariant #N`、`Invariant Labs, 2025` 这类留在仓里或指向外部研究的引用。
- 不动测试注释里的日期(测试记录的是"何时测的",日期是内容)。
- 不动 baselines 里"measured 2026-09-23 on cc-audit v3.23.9"这类第三方工具的探测记录:版本加日期就是那句话的内容。

## 不能说什么

- 不说"注释全部清理完":4 处字符串字面量里的编号还在,列在未决问题 1。
- 压缩后的注释不能少掉任何一条"为什么这个防护存在"的理由 —— review 时逐段对照,不确定的地方列在下面交人先看。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 删 P-/W- 编号和 issues/ 路径,事实留下;`ask.go` 头注释同步压缩(它就是 issues/020 的内容) | `comments: drop proposal/work-item ids and issues/ paths from Go source (P-040)` |
| 2 | HOOK-002 两条 Why 和 LLM-005 的 Why 去掉 spec 括注 | `detect, judge: three user-visible messages stand alone without spec section pointers (P-040)` |
| 3 | 厂商样本名换成形状描述;规划黑话改成现状陈述;非测试注释去日期 | `comments: describe sample shapes instead of naming vendor corpora; drop planning jargon and dates (P-040)` |
| 4 | 十一处长注释压到只剩防护理由 | `comments: compress version chronicles to the design rationale they carry (P-040)` |
| 5 | 本文件、索引一行 | `proposals: P-040 (P-040)` |

W1 无红测试:注释改动没有可钉的行为;反向断言是既有套件绿加 `git diff` 里非注释行的穷举。

## 未决问题

1. **4 处字符串字面量里的编号怎么办?** `hack/gen-rules/main.go:107`(LLM-009 描述,`(P-019)`,会进 `docs/rules.md`)、
   `baselines/cmd/baseline/main.go:343/386/409`(驱动的错误与披露消息)。本条的授权是"只改 3 条字符串",所以留下。
   建议:LLM-009 那句在下一次改规则描述时顺手去掉括注并 `make docs`;baselines 三条改成陈述句(如"the built-in
   default policy is dumped to a file and hashed, so run.yaml carries a hash rather than a version number")。
2. **`ROADMAP B.7` 三处**(`gate.go`、`config.go`、`cmd/aguard/main.go`)按黑话删了:ROADMAP.md 留在仓里,但它自己说
   B.7 "closed as superseded",编号不再指向活条目。若要保留可回退 W3/W4 里那三行。
3. **`hack/corpus-runner` 引用**(baselines 五处)未动:它不是流程文档,但目录已不存在。是否归到 P-038 类"死引用"另行决定。

## 完成

```
合入:PR(2026-09-30;sha 合入后用 git log --grep P-040 找)
发布:待发
证据:P-/W- 编号在 *.go 注释里 0 命中(字符串字面量剩 4,见未决 1);issues/ 路径 0 命中;3 条 Why 不含 "spec §";
     反向断言:git diff origin/dev..HEAD -- '*.go' 的非注释改动穷举为 3 条 Why + 2 条测试失败消息 + 2 个子测试名 + 1 行行尾注释,
     标识符与断言零改动;gofmt -l 为空;make docs 不改 docs/rules.md;go test ./... 30 包绿;make verify "all gates passed"
```
