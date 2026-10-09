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

## 完成的判据

- [ ] `TestGateRemembersAnApprovalAcrossHookProcesses`(`cmd/aguard/gate_pending_test.go`,新):真实 `runHook` 跑两次 ——
  `PreToolUse` 判 `ask`、文件里有这条 pending(前置条件,今天就成立)→ 同一个 `tool_use_id` 的 `PostToolUse` →
  文件里的 approvals 含 `checkTarget` 对同一目录算出的哈希,判决 `accepted-risk`,Post 的输出说 "risk accepted";
  第三次 `PreToolUse`(新 id)输出 0 字节。**今天红**:Post 之后 approvals 是空的
- [ ] `TestPendingSurvivesSaveAndLoad`(`internal/gate/pending_test.go`,新):`pend` → `Save` → `LoadStore` → `pendingFor` 找得到,
  六个字段一个不差。今天红
- [ ] `TestPostPromotesAcrossProcesses`(`internal/gate/pending_test.go`,新):Pre 和 Post 各用一个**从磁盘新读**的 Store 调 `Handle`,
  中间 `Save` → Post 之后已批准,pending 已消费。今天红
- [ ] `TestPendingHygieneOnLoad`(同上,新):一份文件里放好行与坏行 —— 空 id、空 hash、无日期、未来日期、过期(`pendingTTL + 60` 秒前)
  的丢掉;`pendingTTL − 60` 秒前的、新鲜的留下;同一文件里 approvals 的卫生规则照旧(key 与 hash 不符的丢)。
  **为什么是 ±60 秒而不是正好 `pendingTTL`**:跨进程测试跑在墙钟上(`LoadStore` 用进程自己的钟,见未决 1),
  写文件和读文件之间跨一秒就会让"正好 TTL"变成 TTL + 1;精确边界由共用的 `expired()` 和 `TestPendingExpires`(TTL + 1 被修剪)保证
- [ ] 反向断言 (a)/(f) —— 字节变了不提升,而且 Post 是**重算**哈希去比,不是信文件里的哈希:
  `TestChangedBytesAreNotPromotedAcrossHookProcesses`(`cmd/aguard`,新):Pre 之后改 skill 里一个文件 → Post → approvals 为空,
  输出含 "changed between the prompt and the load"。**这句输出是判据的一半**:今天 Post 找不到 pending 就直接返回,"没提升"是白给的;
  有这句话才证明 pending 被找到了、比较真的发生了。
  `TestPendingHashIsComparedNotTrusted`(`internal/gate`,新):手写一份 pending 哈希与扫描结果不同的文件 → Post → 两个哈希都不在 approvals 里
- [ ] 反向断言 (b):`TestExpiredPendingIsNotPromoted`(`internal/gate`,新):扫描器给出与 pending **相同**的哈希,
  pending 停在 `pendingTTL + 60` 秒前 → 跨进程 Post 后不批准;停在 `pendingTTL − 60` 秒前 → 批准(同一条测试里的对照,防止"不批准"是因为别的原因)
- [ ] 反向断言 (c):`TestPostForAnotherCallPromotesNothing`(`internal/gate`,新):Pre `c1` → 新读 → Post `c2` → 不批准;
  再新读,`c1` 的 pending 还在(别人的回答不消费这条)
- [ ] 反向断言 (d):`TestMediumPassIsNotRememberedAcrossProcesses`(`internal/gate`,新):只有 medium 的放行,跨进程 Pre → Post 之后
  approvals 仍为空;`TestPassWithMediumFindingIsNotRemembered` 及旁边三条不改一字仍绿
- [ ] 反向断言 (e):`TestCorruptStoreAsksRatherThanAllows`、`TestKeyMustMatchItsOwnHash`、`TestLoadStore_FIFOReadsAsCorruptNotHang`
  不改一字仍绿;`TestPendingHygieneOnLoad` 另加一例:`pending` 一节类型不对 → 整份 `Corrupt`、approvals 为空
- [ ] 不改一字仍绿:`TestGateAsksThenRemembers`、`TestApprovalOnlyCoversWhatWasShown`、`TestPendingExpires`、`TestDenyParksNothing`、
  `TestFailedToolCallRecordsNothing`、`TestAskEscalatesWhenNobodyWillSeeIt`、`TestGateEndToEnd`、`TestHookRunnerNeverFails`
- [ ] 手跑:`make build` 后两个 `aguard hook` 进程喂同一 `tool_use_id`,修前修后各记一次(本文「问题」一节是修前)
- [ ] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **批准什么不变**:`handlePost` 的"重新解析、重新扫描、哈希一致才提升"一行不动;提升的仍是本进程算出的 `v.Hash`(闸门不变量:
  没有任何 API 接受外来的哈希字符串)。`Verdict.Remembered()`、阈值、`accepted-risk` / `clean` 的区分都不动
- **弹窗文字不变**:`Verdict.Reason()`、`UnrememberedLine()`、Post 的 "risk accepted" / "changed between" 两句、`GATE-000` 的各句都不改 ——
  Post 的那两句今天在生产上**从未出现过**(Post 永远找不到 pending),修完会第一次出现,但文字是原有的(其中一处毛病见未决 5)
- **自动答应模式下 `ask → deny` 不变**;`deny` 仍不 `pend`
- **fail-open 的 `GATE-000` 不变**;30 秒 `scanDeadline`、10 秒 `resolveDeadline` / `preScanDeadline` 不变
- **批准的 key 仍是 canonical 哈希**;pending 的 key 仍是 `tool_use_id`
- **坏文件仍整份读成空**,`Save` 仍拒绝覆盖它;版本号 `storeVersion` 不升(文件格式没变:`pending` 一直在里面,只是没人读)
- **`LoadStore` 的导出签名不变**(五个调用点:`cmd/aguard/gate.go` 四处、`internal/gate/status.go` 一处)
- **不加文件锁**:两个会话同时 load → pend → save,后写的覆盖先写的。这是 approvals 早就有的竞态,丢的方向是"多问一次";
  锁要处理残留锁文件、网络文件系统和超时,而本包头号约束是不能卡住加载
- 不加依赖;不动 `collect` / `detect`(所以不需要真机扫描)

## 不能说什么

- **不说"以前点过的同意现在生效了"**:修之前一条都没写进 approvals,所以修完之后每个 skill 还会再问**一次**,那一次点的同意才被记住。
  在终端里 `aguard approve` 过的不受影响(那条路一直是好的)
- **不说"闸门记住你的每个选择"**:只有判成 `ask`、你点了同意、而且加载时字节没变的才记。拒绝、`deny`、自动模式升级成的 `deny`、
  medium 放行、超过一小时才回答的,都不记,和以前一样
- **不说"已在 Claude Code 会话里实测"**:手跑是两个 `aguard hook` 进程喂**构造**的事件 JSON;真实会话里 Skill 的 `PostToolUse`
  负载长什么样(尤其 `tool_response` 是不是一个对象),本条没有观察到(见未决问题 4)
- **不说"并发安全"**:见「不做什么」最后一条
- **不说"照着 Post 那行提示就能撤销"**:那行里的 `aguard approvals forget a1e9cd5dbc1a…` 带着省略号,原样粘贴会报
  `no approval matches`(见未决 5);能用的是去掉省略号的前缀,或 `aguard approvals` 列出来的那一列

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 跨进程的测试 + 反向断言,跑红 | `gate, cmd: tests — an approval given at the prompt is lost between the PreToolUse and PostToolUse processes (P-007)` |
| 2 | `LoadStore` 读回 pending:空 id / 空 hash / 无日期 / 未来日期 / 过期的丢掉,过期规则与 `pend` 共用一个函数 | `gate: the approvals store reads its pending verdicts back, so PostToolUse in a new process finds the prompt it answers (P-007)` |
| 3 | spec §17、`.claude/rules/gate.md`、`docs/install-gate.md` 与 zh 对子 | `docs: spec §17, the gate rules and the install-gate pair say a parked verdict crosses the process boundary through the file (P-007)` |
| 4 | 本文件、索引 | `proposals: P-007 (P-007)` |

## 未决问题

1. **`LoadStore` 判过期用哪个钟?**
   **建议**:进程自己的 `time.Now()`。`LoadStore` 在 `runHook` 里先于 `Options` 运行,拿不到 `o.Now`;而生产上 `o.Now` 就是
   `nowUnix` = `time.Now().Unix()`,是同一个钟。导出的 `LoadStore(path)` 签名不变,内部转给 `loadStore(path, now)`,包内测试注入时间。
   **已决(2026-10-08)**:按建议。
   实现注记:测试最后没有用到注入的钟 —— W1 的测试必须对着**今天的** API 编译(否则整包编译失败,红的理由就不对了),
   所以全部跑在墙钟上(`wallNow`,边界留 60 秒余量)。没有调用方的 `loadStore(path, now)` 就没拆出来,
   `LoadStore` 内部直接取 `time.Now().Unix()` 交给 `readPending`。钟还是同一个,结论不变。
2. **`asked_at` ≤ 0(无日期)和比现在还晚(未来日期)的行怎么办?**
   **建议**:都丢。两者都不是闸门自己的钟能写出来的(`pend` 在生产上永远拿 `nowUnix()`),而过期规则对它们永远判"没过期",
   于是会永久留在文件里。丢错一条正当的(两次 hook 之间时钟往回拨)代价是多问一次 —— 和坏文件读成空同一个方向。
   **已决(2026-10-08)**:按建议。
3. **`pendingFor` 的注释说"如果没过期",代码其实不查。改代码还是改注释?**
   **建议**:改注释,写明过期在哪两处判(`pend` 写入时、`LoadStore` 读入时)。每个 hook 事件是一个新进程,Store 活不到一条记录在它里面过期;
   给 `pendingFor` 加 `now` 参数在生产上不多拦任何东西,却要改现有测试 `TestPendingExpires` 的调用。
   **已决(2026-10-08)**:按建议。
4. **真实 Claude Code 里 Skill 的 `PostToolUse` 负载能不能过 `Event.succeeded()`?** `succeeded()` 在 `tool_response` 缺失或不是 JSON 对象时
   返回 false,那样修完 pending 也只是活下来、照样不提升。本仓库没有一份真实负载的样本(只有测试里构造的 `{"success":true}`)。
   **建议**:本条不改 `succeeded()`(那是"什么算加载成功"的判断,改它属于另一个决定),在 PR 里列为 AI 不确定的点;
   如果真机证实形状不对,另开 proposal。
   **已决(2026-10-08)**:按建议。
   查证(2026-10-08,Claude Code 官方 hooks 文档 `code.claude.com/docs/en/hooks.md`):`tool_use_id` 在 Pre 与 Post 里都有、同一次调用相同;
   每个事件是一次独立的进程调用;在权限弹窗里被拒的调用不触发 `PostToolUse`。**Skill 工具的 `tool_response` 形状文档没写**,仍未证实。
5. **Post 那行提示里的撤销命令原样粘贴用不了。** "risk accepted … · undo with: aguard approvals forget a1e9cd5dbc1a…"
   里的哈希是 `shortHash` 的输出,带着 `…`;`resolveHashPrefix` 拿 `a1e9cd5dbc1a…` 当前缀,报 `no approval matches`。
   这句话以前从没在生产上出现过(Post 永远找不到 pending),本条修好之后它会第一次出现在用户面前。
   **建议**:本条不改(「不做什么」:弹窗与提示文字不变),由另一份小 proposal 处理(P-008)。本条在「不能说什么」里先披露。
   **已决(2026-10-08)**:按建议(沿用人对本条未决问题"按建议"的预答;它不扩大本条范围,人可在 PR 上推翻)。
