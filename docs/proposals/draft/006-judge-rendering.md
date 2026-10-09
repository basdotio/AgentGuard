<!-- SPDX-License-Identifier: MIT -->
# 006 — 模型的整段 evidence 被渲染成证据,triage reason 能逃出 markdown

- **来源**:判官发现的 snippet 是模型给的整段 evidence,一行真引文加任意编造的行会被当成证据渲染;triage 的 reason 未脱敏、
  能逃出 markdown 代码块;模型回的文本没有长度上限。移植自旧仓 agent-guard 的 P-046(私有仓)
- **依赖**:无
- **分支**:`p/006-judge-rendering`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

证据落地(`ground.go`)查的是"模型引的话在不在发出去的文本里",但查完之后,**报告里印的仍是模型自己写的那段话**。
落地只决定"留不留"和"`file:line` 填哪";渲染出来的 snippet、理由、triage 说明,全是模型输出原样(或几乎原样)进报告。
六处缺口(行号取自 `main` 的 `dec64ca`):

| 缺口 | 现在的行为 | 后果 |
|---|---|---|
| **snippet 是模型的整段 evidence** | `judge.go:172` `finding()` 把 `detect.Redact(v.Evidence)` 整段放进 `Snippet`,`barrierFinding`(`judge.go:196`)同样放整段 quote;`run.go:158` `groundedFinding` 只覆盖 `File`/`Line`。而 `ground()`(`ground.go:44`)整段落不了地时按 `\n` 拆开,**第一行**落地就算通过 | 一行真实的(≥16 字符)加任意多行编造的,整段照印在一个真实的 `file:line` 下面。读者看到的"证据"里有文件里根本没有的行。长度也不设上限:1 MiB 的 evidence 就是 1 MiB 的 snippet,进 JSON / HTML / MD / SARIF |
| **省略标记行能当证据** | `capHeadTail` 在摘录里插一行 `# … N line(s) omitted …`(`excerpt.go:126`),它是发出去的文本的一部分,所以引这一行能落地 | 一条判官发现的"证据"是我们自己插的占位符,`file:line` 指向被省略的那一段的第一行 |
| **理由不设上限** | `Why = detect.Redact(v.Summary)`(`judge.go:157`),不截断、可多行;`def` 只在 `Why == ""` 时用,全是空白的 summary 绕过它 | 模型写多长,报告印多长;一个空白理由印成空行 |
| **triage reason 原样存、原样渲染** | `triage.go:81` `parseTriage` 把 `Reason` 原样存进 `AdvisoryLabel`(不脱敏、不截断);`report/sanitize.go:13` `sanitizeResult` 从不碰 `ArtifactReport.Advisory`;`markdown.go:210` 的 `code(g.Triage)` 假定换行已被清掉 | reason 里一个 `\n\n![x](http://…)` 就逃出代码跨度,贴进 PR 评论就是一个追踪像素;HTML 里 bidi 字符原样通过(不变量 #7 对这一个字段没生效)。`TestMarkdown_AttackerTextIsInert` 不覆盖 triage |
| **两个 clamp 和自己的注释相反** | `clampLabel`(`triage.go:90`)用 `Contains("benign")`;`clampSeverity`(`judge.go:121`)区分大小写 | `"likely-real, not benign"` 被判成 benign —— 注释写的是"未知取 likely-real(安全侧)",实际是往不安全侧偏;`"High"` 变成 medium |
| **SARIF 的规则说明是某个 artifact 的模型理由** | `sarif.go:188` 规则的 `fullDescription`/`help` 取**第一条**同规则发现的 `Why`;指纹(`sarif.go:247`)哈希 snippet | 一条 `LLM-001` 的规则说明是模型对某一个 skill 写的那句话,对同一次扫描里所有 `LLM-001` 都显示它;指纹跟着模型的措辞变 |

受影响的是所有开 `--llm` 的人,以及把 `--md` 贴进 PR 的人。被扫内容按敌对处理、模型按可被劫持处理,
是 §5.2.1 的前提;这几处是这个前提在**渲染**这一侧没落实的地方。

## 初步方向

snippet 改成**落地处那几行发出去的原文**(已脱敏,定长),落在省略标记上的引用按落不了地计入 `LLM-005`;`ground()` 加一个返回落地文本的变体,
老签名包一层不动。`Why` 先脱敏再按 rune 边界截到 512 字节(在共识后缀追加之前),空白理由用规则自己的定义。triage reason 脱敏 + 截 256 字节,
`sanitizeResult` 覆盖 `Advisory`。两个 clamp 改成不区分大小写 / 按首个词精确匹配。SARIF 里判官规则的说明用工具自己写的定义。

动 `internal/judge`(`judge.go` `ground.go` `run.go` 的 `groundedFinding` `triage.go`)、`internal/report`(`sanitize.go` `sarif.go`)。
**不动发出去的内容**(摘录构造、`triageItems`),也不动传输层。
