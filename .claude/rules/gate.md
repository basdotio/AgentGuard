---
paths:
  - "internal/gate/**"
  - "cmd/aguard/gate*.go"
  - "hack/pre-commit"
---
<!-- SPDX-License-Identifier: MIT -->
## 加载时闸门(`internal/gate`)

`aguard hook` 注册成 Claude Code 的 hook,在 agent **加载**一个 skill 之前跑一遍 `check` 的静态路径。
面向用户的说明在 [docs/install-gate.md](../../docs/install-gate.md)(中文版 `.zh-CN.md`);这里只写不要怎么改。

- **名字叫"加载时"而不是"安装时",是结论不是措辞。** Claude Code 只有九个 hook 事件,**没有一个在
  安装时触发**;就算有也覆盖不全 —— skill 还可以 `git clone`、`cp`、手动放进去。守得住的是加载,
  而它恰好就是要紧的那条边界:磁盘上的 skill 是惰性的。这跟 `collect/unowned.go` 的判据是同一条
  ("agent 有没有加载路径进得去")。**别把这个包改回去拦安装动作** —— 那条路没有挂载点。
- **批准的 key 是 canonical 哈希,永远不是名字或路径。** 这是整个设计成立的原因:改一个字节哈希就变,
  闸门自己重新问,没有过期时间要调、没有缓存要失效。**任何"按名字记住"的优化都会重新打开"换掉内容、
  留着名字"这个规避**,而那是成本最低的一种。
- **只有 `PreToolUse[Skill]` 能真的拦住东西**,但插件自带的 hook 和 MCP server 从会话第一轮就是活的,
  根本没有加载事件。所以 `SessionStart` 那半必须在,而且它的消息里**必须继续写着"这些没有被拦住"**
  —— 只发前一半会让运维以为整个环境都被守住了,而实际上只有 skill 是。有测试盯着这句话
  (`TestSessionStartSaysWhatItCannotGate`)。
- **覆盖边界那句话在"没有告警"时也必须发出去**(`quietCoverageMessage`)。以前它是**挂在告警块上**的,
  于是受众正好被反过来了:最需要知道"hook 和 MCP 没被守"的人,恰恰是环境干净、因此最容易断定闸门
  全都管了的那些人 —— 而他们是唯一收不到这句话的人。真机上找出来的:最高发现只有 medium,
  `SessionStart` 返回 **0 字节**。`TestSessionStartSaysWhatItCannotGate` 抓不到,因为它先塞了一条
  `SevHigh`,只走过响的那条路;补的是 `TestSessionStartStatesCoverageWhenQuiet`。
  干净路径**不套 `gateNote`**(`GATE-000` 的意思是"有东西审不了",盖在正常启动上等于把一切正常
  渲染成一次失败),**也不进 `AdditionalContext`** —— 受众是运维、文本是固定串、没有攻击者写的字,
  所以既不需要围栏,也没有理由花会话上下文去告诉模型闸门管不着什么。`resume`/`compact` 仍然整段跳过。
- **hook、MCP、permission 现在也能被"认识"了(P-009),但入口没变。** 它们以前哈希是 `""`,`Approved("")` 恒 false,
  于是 `SessionStart` 永远列它们,`aguard approve <root>` 在最差 artifact 是它们时打印 `approved` 却什么都没存
  (`Store.Approve` 丢掉空 key)。现在它们有内容哈希(`.claude/rules/hash.md`),同一条 `approve` 真的存下,`SessionStart`
  随后跳过 —— 直到配置或 hook 跟进的脚本改了一个字节(`TestGate_ApprovedHookLeavesSessionStart` 两个方向都钉)。
  **本包代码一行没动**:没有新的写批准路径,`SessionStart` 仍只告知。**例外要知道**:哈希换掉了 secret,只改 secret
  不重键 —— 对 hook 这意味着批准覆盖"同一条命令、不同密码";`PARSE-000` 的 artifact 仍是 `""`,`approve` 对它们仍会
  空打印一句 `approved`(由 P-011 单独处理)。
- **批准只能覆盖"给人看过的那份字节"。** `PreToolUse` 把判决按 `tool_use_id` 停在 `pending` 里,
  `PostToolUse` 重新读一遍目标、哈希仍然一致才提升为批准。**不要图省事直接在 PostToolUse 记当前哈希**
  —— 那样目标在弹窗和加载之间被换掉,就会拿一个针对别的内容的"同意"去认证它。
- **pending 只活在文件里**(2026-10-08,P-007)。Claude Code 每个 hook 事件起一个**新进程**,`PreToolUse` 停下的判决
  要到另一个进程的 `PostToolUse` 才兑现,两者之间只有 approvals 文件。`LoadStore` 以前只读 `approvals` 不读 `pending`,
  于是**弹窗里的同意从没被记下过**,Post 还一个字都不说 —— 而全部测试是绿的,因为它们把**同一个内存 Store** 交给 Pre 和 Post。
  **测 Pre→Post 的测试必须让每个事件从磁盘新读一个 Store**(`internal/gate/pending_test.go` 的 `nextProcess`;
  `cmd/aguard/gate_pending_test.go` 走两次 `runHook`)。读回的卫生:空 id、空哈希、无日期、未来日期、过期的行丢掉;
  过期规则只有 `expired()` 一份,`pend` 修剪和 `LoadStore` 读回都调它 —— **不要在别处再写一遍 `now-asked > TTL`**。
- **放行不等于记住**(2026-09-16,P-005)。`Verdict.Remembered()`:有 medium 及以上确定性发现的放行**不写 approvals**,
  每次加载重审并出 `UnrememberedLine`(规则 ID、不带 snippet、给出 `aguard approve` 那条命令);干净和只有 low 的才记。
  **不要把这条并回阈值** —— "拦不拦"和"记不记"是两个决定,P-005 之前它们是一个:四个 ToB 样本各以一条 medium 通过
  high 阈值,然后被永久信任,以后同样字节连问都不问。也**不要把 low 也变成不记**:大多数真实 skill 会每次加载都响,
  那是让人卸闸门的成本。`TestPassWithMediumFindingIsNotRemembered` 及旁边三条钉住边界两侧。
- **没有哈希就没有批准,而且要拒绝得出声**(P-011)。`Store.Approve` 遇到空 key 静默丢掉 —— 对批准库这是对的
  (读不出哈希的东西就是没人审过的东西,`TestEmptyHashIsNeverApproved`),但调用方不能因此照常报喜:`aguard approve`
  以前在最差 artifact 哈希为空(`PARSE-000`、打不开的文件)时打印 `approved … hash ` 然后什么都没存,人以为信任了,闸门照问。
  现在 `approvePath` 在**打开批准库之前**看 `Verdict.Hash`,空就返回运行错误(退出码 2),原因取 `Verdict.Unhashed`
  —— 由 `Summarize` 填,因为只有它知道选的是哪个 artifact。**不要按 kind 猜原因**:kind 会获得哈希,猜的那句当天变假话。
  **不要改成退到下一个有哈希的 artifact**:那是批准一份判决没描述的内容。`TestApproveRefusesWhatHasNoContentHash`、
  `TestApproveRefusalLeavesTheStoreAlone` 钉拒绝,`TestApproveStillRecordsWhatHasAHash` 钉反方向,`TestApproveNeverFallsBackToAnotherArtifact` 钉不退。**闸门自己的 PreToolUse「干净就记住」那一支同理**:哈希为空时不说 trusted、不报 store 变更,说没记住和原因(`Verdict.UnhashedLine`;`TestCleanLoadWithNoHashIsNotClaimedTrusted`,反向 `TestCleanLoadWithAHashIsStillRemembered`)。
- **`ask` 在会自动答应的权限模式下等于放行,所以要升级成 `deny`。** `auto`/`acceptEdits`/
  `bypassPermissions`/`dontAsk` 会自动答应弹窗;闸门读事件里的 `permission_mode`,命中就改判拒绝,
  并在理由里点名是哪个模式。**这是真机上找出来的**:一个 51/100、带完整凭证外泄链的 skill 返回了
  `ask`,会话自动接受,skill 照常加载,全程零显示 —— 一个决定被吞掉的闸门是在**无声地** fail open,
  而下一条说了这个包唯一不许出现的就是"安静"。`default`/`plan` 不动;**不认识的模式一律不升级**,
  否则 Claude Code 每出个新模式就变成一堵拒绝墙。
- **插件的子项(`Plugin` 字段非空,P-044)永远不是判决里的那个 artifact,也不进 SessionStart 的列表。** `Summarize` 选"最差 artifact"时
  跳过它们 —— 选中一个分更低的子项,`aguard approve <插件>` 就会把子项的哈希存成对插件的批准;SessionStart 跳过它们,因为子项的每条
  阻断规则都已经在插件那一行上。另一头是对齐的:`PreToolUse[Skill]` 解析 `plugin:skill` 得到的目录和哈希,正是扫描报告里那个子项的
  (`TestGate_NamespacedSkillHashIsTheChildsHash`),所以加载时记下的批准,和报告里点名的是同一样东西。
- **只有判成 `ask` 时才 `pend`。** 拒绝没有"你答应了"这条路径,停一条永远兑现不了的判决,是在一个
  每次风险加载都会写的文件里攒垃圾。
- **`systemMessage` 在 VSCode 扩展里不渲染**(实测),所以干净路径那行提示可能根本没人看见。
  **不要为此改用非零退出码 + stderr** —— 那会把"一切正常"渲染成一条错误,比沉默更糟。要给用户
  反馈就走 `aguard hook status` 和 `aguard approvals`,那是他主动问的时候。
- **`scan` 会报"注册了但已死"(`GATE-001`,维度 0)。** 放在 `scan` 而不是只留给 `hook status`,
  是因为没人会定期跑一个状态命令,而 `scan` 是大家本来就在跑的。**它不计分** —— 死掉的 hook 让
  *报告*不可信,不让任何 artifact 更危险;把它做成计分发现会污染那个必须一直表示"扫描在你的
  artifact 里发现了什么"的数字。注入点在 `scan` 命令里、`score.Apply` 之后,这是安全的:
  `score.Deterministic` 排除维度 0,所以它既不动分数也不动 `--fail-on`。
- **闸门等自己的扫描器必须有 deadline**(`scanDeadline` = 30s,`withDeadline` 包住全部三处
  `o.Scan`/`o.ScanRoot`)。本包在审的时候是**阻塞**的,所以任何不返回的扫描都会把加载一直拖住;
  拖到编辑器自己的 hook 超时之后,skill 照常加载而**一个字都没说** —— 那是**静默地** fail open,
  正是下一条明令不许的形状。FIFO 是这件事的一个**原因**,不是这条性质本身:巨大的树、卡住的网络
  文件系统、reader 里的下一个 bug,结局完全一样。超时走的是**同一条** `unaudited()` —— 于是运维
  看到的仍然是那条 `GATE-000`,不需要第三种"我们审不了"的说法。30s 是对着实测 ~0.75s(真机全量
  `scan`)定的,约 40 倍余量;**宁大勿小** —— 提前到点会把"扫描器只是慢"报成"这个 artifact 没被审过",
  而"没被审过"是一句需要有把握才能说的话。`withDeadline` 的时长是**参数不是常量**,否则测试要真等
  30 秒。被放弃的那个 goroutine 不取消:要取消就得把 context 一路穿进 collect 和 detect,比这个修复
  本身大得多,而 hook 进程写完判决就退出,goroutine 跟着一起死;channel 带缓冲正是为此 —— 一个没人
  接的 send 不能把那个 goroutine 卡在答案之后。
- **本包 fail-open,这是刻意与工具其他部分相反的。** 名字解析不了、扫描失败、采集为空、状态文件写不
  进去,一律**放行并出 `GATE-000`**。不变量 #2 的 fail-closed 是对的 —— 在扫描器内部;放到这里就变成
  "aguard 一有 bug,编辑器就加载不了 skill",那种闸门当天就会被卸掉,卸掉之后保护为零。**唯一反向的
  例外是坏掉的 approvals 文件:它读成空**(于是什么都问一遍),因为丢批准只是多弹窗,而信任一个读不懂
  的文件是白送一次静默放行。`Save` 也拒绝覆盖它。
- **进模型上下文的东西必须过 nonce 围栏,证据 snippet 一律不进。** artifact 名字和路径是攻击者写的,
  所以 SessionStart 注入的那段套 `fence()`(与 judge 同一个装置,`crypto/rand` 失败就整段不注入,
  绝不退化成固定围栏)。`Verdict.Reason()` 只带规则 ID 和 `file:line`,**不带 snippet** —— 把被审文件
  的原文塞进一条安全提示再喂给模型,正是这个工具拒绝采取的形状。有测试盯着
  (`TestReasonCarriesNoEvidenceSnippets`)。
- **要复制粘贴的命令只能带 `CommandArg(v.FullPath)`,不能带 `v.Path`,也不能用 `%q`**(P-030)。`v.Path` 是截到
  160 字符的显示形态,贴进 `aguard check` 指向一个不存在的目录(桌面版装的 skill 路径通常更长);`%q` 不是 shell
  引号,`$HOME`、反引号、`$(…)` 照样被展开,粘贴就执行了路径里的一段。给人读的那行保持截短。
  `TestGateCommandsPasteAsPrinted` 用真 `/bin/sh` 展开每一条命令,再喂给真的命令函数。
- **判定必须走 `score.Deterministic`,不要在这里重写一遍条件。** 闸门和 `--fail-on` 必须对同一份
  字节给同一个答案;judge 在这条路上永不运行。`check` 自 P-004 起接受 `--llm`,**闸门不跟**:`gateOptions` 和
  `approvePath` 构造的 `scanOpts` 永不设 `llm`,`TestGateScannerNeverEnablesLLM` 用计数端点钉住(比检查字段更严:
  换一种方式在闸门里打开判官也会红)。闸门对应的是不带 `--llm` 的 `check`。
- **`hook uninstall` 要删掉每一份、且只删我们自己的**(`removeCommand`,2026-09-08)。原来 `findEntry` 只返回第一个匹配、
  只删那一条:闸门被注册了两次(老安装路径、手改)时,uninstall 报成功、hook 照样触发。而且它删的是**整条 entry**——一条 entry 的
  inner hooks 里我们的命令和运维自己的命令共用同一个 matcher 时,把人家的也一起删了。现在遍历全部条目、只摘我们的 inner 命令、
  条目空了才删,`Removed` 标签带 `(×N)`。`TestUninstallRemovesEveryCopyAndOnlyOurs` 钉住两半。
- **备份是两个槽位,原始文件永不覆盖**(2026-09-08)。原来只有一个 `.aguard-bak`:install 存原始、uninstall 又把 install 后的状态
  写上去,两条命令之后运维"aguard 碰之前的 settings"就没了。现在 `.aguard-bak` 只在**不存在时**写一次(那就是原始),
  `.aguard-bak.prev` 每次覆盖存最近一次改动前的状态。`Describe` 分别说明两个;`collect/unowned.go` 按前缀豁免两个槽位
  (`TestBackupKeepsTheOriginal`)。
- **`hook install` 只能合并,不能覆盖。** settings.json 是运维自己的,里面有跟本工具无关的 permission
  和 hook;读不懂的 settings 一律报错而**不是**换成一份只有我们 hook 的新文件 —— 那会静默删掉他所有
  权限配置。另外三个事件共用一条命令(runner 按 `hook_event_name` 分派),**别改成 shell 管道** ——
  那样闸门的安装动作会被 `HOOK-001` 报出来,一个自己触发自己告警的安全工具会教会用户忽略告警。

