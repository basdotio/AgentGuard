<!-- SPDX-License-Identifier: MIT -->
# 008 — 闸门批准后给的撤销命令照抄就失败:哈希后面带着省略号

- **来源**:接受风险后的提示 `undo with: aguard approvals forget <12位>…` 照抄运行会失败;另外 `forget ""` 在只有一条批准时会把它删掉。
  移植自旧仓 agent-guard 的 P-050(私有仓)
- **依赖**:无(与 P-007 独立可合;没有 P-007 时这条提示在真实的两进程 hook 里走不到,但代码与测试不依赖它)
- **分支**:`p/008-undo-hint-pastes`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

PostToolUse 记下一次"接受风险"的批准后,闸门给出(`internal/gate/hook.go:345-347`):

```
AgentGuard: risk accepted for skill "pdf-export" (51/100) · content a1e9cd5dbc1a… · undo with: aguard approvals forget a1e9cd5dbc1a…
```

撤销命令里的哈希是 `shortHash` 的输出:前 12 位**加一个 `…`**。照抄运行,`aguard approvals forget a1e9cd5dbc1a…`
把 `…` 当成哈希前缀的一部分,`resolveHashPrefix`(`cmd/aguard/gate.go:429`)找不到匹配,报 `no approval matches` 退出 2。
`forgetApproval` 的注释(`cmd/aguard/gate.go:415`)自己写着"接受前缀,因为消息里打印的就是前缀"——但消息里打印的不是前缀,是前缀加省略号。
SessionStart 和弹窗里的 `content hash` 也是同一个 `shortHash` 形式(`internal/gate/gate.go:179`、`:190`),从那里复制同样失败。

顺带的第二个缺陷:空字符串是任何哈希的前缀,`resolveHashPrefix` 没有拒绝它。`aguard approvals forget ""` 在批准库里恰好只有一条时,
不问一句就把它删掉。

用 `main`(`dec64ca`,v0.18.0)构建的二进制对一个临时 root 手跑(`aguard approve` 记一条,再按消息里的形式撤销):

```
aguard approvals forget bede3b30172e…    → error: no approval matches "bede3b30172e…"   exit 2
aguard approvals forget bede3b30172e...  → error: no approval matches "bede3b30172e..."  exit 2
aguard approvals forget ""               → forgot bede3b30172edde57545241d7d19b1214a041489ce7f2ec328a82d62a77e7774;
                                           the gate will ask about that content again   exit 0
aguard approvals                         → no approvals recorded
```

后果:用户在弹窗里点了"接受",想反悔时,工具给的唯一一条反悔命令是坏的;而一条手滑的空参数会静默撤掉一条批准。
在本仓库 `main` 上,这条撤销提示在真实的两进程 hook 流程里还走不到(PostToolUse 读不回 PreToolUse 停下的 pending,见 P-007);
P-007 合入后它才第一次会被人看到,所以至今没人照抄过它。

## 初步方向

撤销命令里打印**可以直接粘贴**的前缀(不带 `…`);展示用的 `content …` 不动。`forget` 顺带容忍末尾的 `…` / `...`
(从别的消息里复制来的短哈希同样带省略号),仍然要求唯一匹配,并拒绝空前缀。

## 完成的判据

- [ ] `TestUndoHintPastesAsIs`(`cmd/aguard/gate_undo_test.go`,新):走一遍 Pre → Post 拿到"接受风险"的 SystemMessage,把 `undo with:` 后面的命令
  **原样**拆成参数交给 `forgetApproval` → 成功,批准被删除。今天红:`no approval matches "…"`
- [ ] `TestForgetAcceptsTheShortHashAsPrinted`(同文件,新):`a1e9cd5dbc1a…` 和 `a1e9cd5dbc1a...` 都解析到唯一那条批准
- [ ] 反向断言:前缀匹配多条仍然报 "matches N approvals",一条都不匹配仍然报 "no approval matches";`forget all` 行为不变
- [ ] 反向断言:**空前缀**(`""`,或去掉省略号后为空的 `…`)报错,不删除任何东西——今天 `forget ""` 在只有一条批准时会把它删掉
- [ ] 反向断言:提示里展示用的 `content <12 位>…` 不变;`TestGateAsksThenRemembers`、`TestApprovalOnlyCoversWhatWasShown` 不改一字仍绿
- [ ] `make verify` 绿

## 不做什么

- 不改哈希展示长度(12 位)和 `aguard approvals` 列表的 16 位
- 不改批准的语义、存储格式、提示的其余文字
- 不碰 P-007 要改的 `LoadStore`(`internal/gate/approvals.go`)

## 不能说什么

- 不说"撤销命令以前就能用":P-007 之前它在真实 hook 流程里根本不会出现,P-007 之后照抄就失败
- 不说前缀永远唯一:12 位前缀撞车时 `forget` 报错要求更多字符,这是既有行为

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 两条新测试 + 空前缀反向断言,跑红 | `cmd: tests — the undo command the gate prints fails when pasted, and an empty prefix forgets an approval (P-008)` |
| 2 | 提示里的撤销命令打印不带省略号的前缀 | `gate: the undo hint prints a prefix that can be pasted as it is (P-008)` |
| 3 | `forget` 去掉末尾的 `…` / `...`,拒绝空前缀 | `cmd: approvals forget takes a short hash as printed and refuses an empty prefix (P-008)` |
| 4 | install-gate 对子说明 `forget` 接受前缀 | `docs: install-gate says forget takes the prefix the messages print (P-008)` |
| 5 | 本文件、索引 | `proposals: P-008 (P-008)` |

## 未决问题

1. **提示里打印完整哈希还是 12 位前缀?**
   **建议**:12 位前缀,不带省略号。完整 64 位哈希塞进一行系统消息太长;12 位在一台机器的批准库里撞车的概率可以忽略,撞了 `forget` 会报错要求更多字符。
   **已决(2026-10-08)**:按建议(旧仓已决,移植沿用)。
2. **`forget` 要不要也容忍省略号?**
   **建议**:要。SessionStart 和弹窗里的 `content hash` 也是 `shortHash` 带省略号的形式,用户从那里复制同样会失败;去掉末尾的 `…` 或 `...` 再按前缀匹配,唯一性要求不变。
   **已决(2026-10-08)**:按建议(旧仓已决,移植沿用)。
3. **最短前缀要不要设下限(比如 4 位)?**
   **建议**:不设,只拒绝空前缀。非空前缀不唯一时既有逻辑已经报错。
   **已决(2026-10-08)**:按建议(旧仓已决,移植沿用)。

## 完成

```
合入:PR #17(2026-10-09;sha 用 git log --grep P-008 找)
发布:待发
证据:TestUndoHintPastesAsIs(cmd/aguard/gate_undo_test.go);W1 红:the undo command as printed failed: no approval matches "a1e9cd5dbc1a…" → W2 后绿,照抄的命令删掉了批准,展示用的 content a1e9cd5dbc1a… 不变
证据:TestForgetAcceptsTheShortHashAsPrinted(同文件);W1 红(… 与 ... 两种都 no approval matches)→ W2 后仍红(W2 只改提示)→ W3 后绿
证据:反向断言 TestForgetRefusesAnEmptyPrefix(同文件);W1 红:forget "" succeeded 并 withdrew the approval(既有的隐患)→ W3 后 ""、…、... 全部拒绝,批准仍在
证据:反向断言 TestForgetStillRequiresAUniqueMatch(同文件,随 W3 加入):共享 12 位前缀的两条 → matches 2 approvals;不匹配 → no approval matches;两条都在
证据:反向断言不改一字仍绿 —— go test -race ./internal/gate/ ok(TestGateAsksThenRemembers、TestApprovalOnlyCoversWhatWasShown 等;gate_test.go、hook_test.go 无改动)
证据:二进制对临时 root 手跑 —— main(dec64ca):forget <12位>… 与 <12位>... 都 no approval matches 退出 2,forget "" 退出 0 并删掉唯一那条批准;本分支:forget "" 与 forget … 报 no hash given 退出 2、批准仍在,<12位>... 与 <12位>… 都 forgot 退出 0,forget all 对两条批准 forgot 2 approval(s)
证据:不做什么 —— git diff --stat origin/main -- internal/gate/approvals.go internal/gate/gate_test.go internal/gate/hook_test.go go.mod go.sum 为空
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5
```
