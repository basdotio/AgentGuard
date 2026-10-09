<!-- SPDX-License-Identifier: MIT -->
# 007 — 在闸门弹窗里批准过的 skill,下次加载还会再问:批准从来没被记下来

- **来源**:每个 hook 事件是一个新进程,`LoadStore` 不读回 pending,弹窗里的批准在 `PostToolUse` 时已经丢了;
  既有测试在同一进程里共用内存 store,一直是绿的。移植自旧仓 agent-guard 的 P-049(私有仓)
- **依赖**:无
- **分支**:`p/007-gate-pending-survives`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

闸门对一个带 high 发现的 skill 返回 `ask`,运维在弹窗里点了同意,skill 加载。用户文档
(`docs/install-gate.md` 的事件表)说这时 `PostToolUse` 会"记下来",以后同样的字节不再问。**实际上从来没有记下来过**:
下一次加载同一份字节,弹窗照样出现,`aguard approvals` 是空的。

原因在进程边界上。Claude Code 的每个 hook 事件都是**一个新进程**:

| 进程 | 做什么 | 实际结果 |
|---|---|---|
| `PreToolUse` | 判成 `ask`,`Store.pend` 按 `tool_use_id` 把判决停进 `pending`(`internal/gate/approvals.go:139`),`Save` 写盘 | 文件里确实有这条 pending |
| `PostToolUse` | `LoadStore` 读文件(`approvals.go:96`),`handlePost` 按 `tool_use_id` 找 pending(`internal/gate/hook.go:314`),重扫、哈希一致就提升为批准 | `LoadStore` 只把 `on.Approvals` 抄进新 Store(`approvals.go:115-124`),**`Pending` 从不读回**。pending 找不到,`handlePost` 直接返回空,什么都不记,**也什么都不说** |

还有一个连带后果:任何一次后续写盘(下一个 `PreToolUse`、`aguard approve`、`aguard approvals forget`)都用这份
没有 pending 的 Store 覆盖文件,所以别的会话里**正开着的**弹窗,它的 pending 也被一并抹掉。

用构建出的二进制对一个临时 root 手跑,每步一个 `aguard hook` 进程(2026-10-09,`main` 的 `dec64ca`):

```
1. PreToolUse  toolu_repro1  → decision: ask;store 里有 pending.toolu_repro1(hash a1e9cd5d…,51/100)
2. PostToolUse toolu_repro1  → stdout 0 字节;store 不变,approvals 仍是 {}
   aguard approvals          → no approvals recorded
3. PreToolUse  toolu_repro2  → decision: ask(同一份字节,又问了一遍)
```

为什么测试全绿:`internal/gate` 里覆盖这条路径的测试(`TestGateAsksThenRemembers`、`TestApprovalOnlyCoversWhatWasShown`)
让 Pre 和 Post **共用同一个内存里的 Store**,于是 pending 从没经过磁盘;`cmd/aguard` 的端到端测试
(`TestGateEndToEnd`)走真实的 `runHook`,但只发 `PreToolUse`,从没发过 `PostToolUse`。

后果:

- **"弹窗里同意一次就够"这个承诺不成立**。每次加载一个带 high 发现、运维已经决定接受的 skill,都会再弹一次;
  而运维会把这读成"闸门坏了",这正是 `.claude/rules/gate.md` 反复说的、让人卸掉闸门的那种成本。
- **而且是静默的**:PostToolUse 什么都不输出,运维没有任何途径知道自己的回答被丢了。唯一能绕开的办法是
  在终端里 `aguard approve <path>`,而文档没有告诉他要这么做。
- 文件里的 pending 不会自己长大(下一次 `pend` 会整份覆盖),但每一条都只是写下去、从没被读过。

## 初步方向

`LoadStore` 把 `pending` 读回来,卫生规则与 approvals 同一套思路:key 为空、hash 为空的行丢掉;过期的
(与 `pend` 里修剪用的**同一条**过期规则)丢掉;读不懂/未知版本的文件仍整份退化成空。只动 `internal/gate/approvals.go`
和它的测试;`handlePost` 的"重读、重算哈希、一致才提升"不动 —— 那正是 pending 存在的理由。
补一条跨两个进程的测试(两次 `runHook`),这是现有测试结构上造不出的场景。
