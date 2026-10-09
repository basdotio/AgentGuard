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
