<!-- SPDX-License-Identifier: MIT -->
# 013 — settings.json 解析失败时,终端和 markdown 报告说 "looks safe":artifact 自己的 dim-0 note 从不渲染

- **来源**:collect 把 `PARSE-000` 挂在 artifact 上而不是扫描级 note,终端、markdown、HTML 只渲染扫描级 note,
  于是一个解析不了的 `settings.json` 被报成 "looks safe … Nothing was found to check"(不变量 #5:任何遗漏都不许静默)。
  移植自旧仓 agent-guard 的 P-054(私有仓)
- **依赖**:无
- **分支**:`p/013-artifact-notes-rendered`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

一个 `settings.json` 写坏了的 `.claude`(内容 `{"hooks": {"PreToolUse": [ broken`),用 `main`(`dec64ca`,v0.18.0)
构建的二进制实测:

```
$ aguard check …/broken/.claude
AgentGuard scan · root=…/broken/.claude
Risk score 100/100 (Low)

Summary
  Your Claude Code setup looks safe. No findings.
  Nothing was found to check under this root.


✅ No risk findings (static).

Scan details
  …
  Inventory: skills=0 mcp=0 hooks=0 permissions=0 subagents=0 commands=0 plugins=0 connectors=0
$ echo $?
0
```

`--verbose` 与默认输出一字不差;`--md -` 同样是 "looks safe" + "Nothing was found to check",没有 "Not checked" 一节;
`scan --root … --html`(`check` 没有 `--html`)同样,页面里 `PARSE-000` 出现 0 次。而 `check --json` 里那条说明就在:

```
artifacts: [("hook", "settings.json", hash "", score 100, findings [("PARSE-000", dimension 0, "Parse failed, artifact not fully covered")])]
notes: []
```

`check --sarif` 也带着它(`PARSE-000`,`properties.artifact = "hook:settings.json"`)。

原因:`collect.withParseError`(`internal/collect/collect.go:580`)把 `PARSE-000` 挂在**它造出来的那个 artifact 上**,不放进
扫描级 `notes`。三个给人读的渲染器只从 `ScanResult.Notes` 取 dimension-0 说明(`text.go:167` 的 `writeNotes(w, r.Notes, …)`、
`markdown.go:79` 与 `html.go:264` 的 `splitNotes(r.Notes)`),而 `report.Aggregate`(`aggregate.go:86`)跳过所有 dimension-0
的发现 —— 于是 artifact 自己的 note **哪里都不印**。SARIF 印了(它遍历每个 artifact 的 findings),JSON 印了(原样序列化),
给人看的三个都没有。

同一份配置里装着 hooks、permissions、env,是这个工具最在意的那块面;它没被读,报告却说 "looks safe"、"Nothing was found to
check"(找到了,只是没读成)。这正是不变量 #5("任何遗漏都不许静默")要防的事:说明产出了,读者看不到。README 里
"`--json` / `--html` / `--md` always carry everything"(`README.md:186`)对 html 和 md 也不成立。

`withParseError` 有三个调用点:`settings.json` / `settings.local.json`(hook 类 artifact,`collect.go:540`)、MCP 配置
(`~/.claude.json` 等,mcp 类,`collect.go:476`)、`plugins/installed_plugins.json`(plugin 类,`plugins.go:144`)。
`main` 上实测三种都是同一个缺口:坏的 `~/.claude.json`(`{"mcpServers": {`)和坏的 `installed_plugins.json` 同样报
"looks safe … Nothing was found to check",JSON 里各有一个带 `PARSE-000` 的 artifact、`notes` 为空。

退出码 0 本身不是这次要改的:dimension-0 note 从不参与 `--fail-on`(`score.Deterministic`),这是不变量 #4 的一部分。

## 初步方向

只改给人读的三个渲染器(`internal/report` 的 text / markdown / html):artifact 自己的 dimension-0 note 和扫描级 note 进同一条
"Not checked" 通道(同一行折叠、同一段 `--verbose`、同一个 `<details>`、同一个 HTML 区块)。数据不挪 —— JSON / SARIF
已经带着它,字节不变。Summary 里的两句("looks safe"、"Nothing was found to check")要知道覆盖不全,措辞沿用现有的
"coverage is incomplete"。分数、退出码、`--fail-on` 都不动。

## 完成的判据

夹具是同一个 `.claude` 的两份 `settings.json`:**坏的** `{"hooks": {"PreToolUse": [ broken`,**好的**是同一个开头写完整、
注册一条 `echo ok` 的 hook(`main` 上 100/100、零发现、零 note,输出 "looks safe" + "Checked 1 hook.")。

- [x] `TestBrokenSettingsIsNotReportedSafe`(`cmd/aguard/artifact_notes_test.go`,新):坏夹具分别走 `scanEnv` 和 `checkTarget`,
  四个给人读的渲染器(终端默认、`--verbose`、markdown、HTML)每一个都含 `PARSE-000` 和 `settings.json` 的路径,
  每一个都**不含** `looks safe` 和 `Nothing was found to check`。今天红:四个渲染器都没有 `PARSE-000`
- [x] `TestCheckBrokenSettingsCLI`(同文件,新):真二进制 `aguard check <坏夹具>` 的 stdout 含 `PARSE-000`、不含 `looks safe`,
  **退出码 0**。今天红在 stdout 那半;退出码那半今天就是 0,修完仍是 0
- [x] `TestArtifactNoteReachesEveryHumanRenderer`(`internal/report/artifact_notes_test.go`,新):手搭的结果,artifact 自带一条
  dim-0 note,文件名里带 U+202E 和 ESC;四个渲染器都显示这条 note,且输出里**没有** U+202E / ESC(不变量 #7,
  文件名是被扫目录给的)。今天红:note 不显示
- [x] `TestSummaryDoesNotCallIncompleteCoverageSafe`(同文件,新):Low 档 + **Claude Code 加载的东西没读全**(人定的集合,
  见未决问题 2:挂在 artifact 上的覆盖 note,或任何位置的 `IO-000` / `PARSE-000`)→ 三个渲染器的 Summary 都说
  `coverage is incomplete`、不说 `looks safe`。今天红。另有三行:只有"顶层条目没读"的扫描级 `COV-000` → 仍说
  `looks safe`;只有 `LLM-002` → 仍说 `looks safe`;扫描级 `PARSE-000`(hook 条目没看懂)→ 对冲
- [x] `TestUnownedEntriesKeepTheHeadline`(`cmd/aguard/artifact_notes_test.go`,新):真实流水线,好夹具 + 一个
  `sessions/a.jsonl` → 恰好一条扫描级 `COV-000`、Low 档;四个渲染器都仍是 `Your Claude Code setup looks safe.`,且这条
  note 仍然披露(终端的 "Not checked —" 行、`--verbose` 的 "⚠ Scan warnings"、markdown 的 "## Not checked"、HTML 的
  `notchecked` 区块)
- [x] 反向断言 `TestUnreadableSettingsHedgesTheHeadline`(同文件,新):`chmod 000` 的 `settings.json` → 扫描级
  `IO-000`、没有 artifact → 四个渲染器的头条都对冲(以 root 运行读得到时 skip)
- [x] 反向断言 `TestCleanSettingsReportIsUnchanged`(`cmd/aguard/artifact_notes_test.go`,新,今天就绿):好夹具的终端默认、
  `--verbose`、markdown 输出与**本仓 `main` 上录下**的 golden **逐字节相同**(只把临时目录替换成占位符、把时间和版本固定);
  HTML 说 `looks safe`、`Checked 1 hook.`、没有 "Not checked" 区块。修完不改一字仍绿
- [x] 反向断言 `TestBrokenSettingsMachineOutputUnchanged`(同文件,新,今天就绿):坏夹具的 JSON 里 `PARSE-000` 仍在
  `artifacts[0].findings`、`notes` 仍为空,SARIF 仍有这条结果且归属 `hook:settings.json` —— 数据没挪;分数 100 不变;
  `failGate` 在 `--fail-on high` 与 `--fail-on low` 下都放行(dim-0 不 gate)。修完不改一字仍绿
- [x] 反向断言:Low 档、没有覆盖 note → 仍是 `Your Claude Code setup looks safe.`;只有压制类 note(`REP-GOOD`/`IGN-000`)
  → 仍是 `looks safe`(压制不是覆盖缺口,Summary 已有自己的一行);`TestVerdictSentence`、`TestCheckedLine` 不改一字仍绿
- [x] 真机:坏夹具和好夹具在 `main` 与本分支的 `check --json` / `check --sarif` 输出(去掉 `scanned_at`)`diff` 为 0 字节;
  只带"顶层条目没读"的夹具和空 root 的终端输出与 `main` 逐字节相同;真机 `~/.claude` 输出不变(若它不在 Low 档或没有
  artifact 自带的 note)
- [x] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不挪数据**:`collect.withParseError` 照旧把 `PARSE-000` 挂在 artifact 上;`model`、`collect`、`detect`、`score`、
  `cmd/aguard/main.go` 的 `analyze` 一行不动。JSON / SARIF 的渲染(`internal/report/sarif.go`、`main` 里的 JSON 编码)不动
- **不改退出码、不改 `--fail-on` / `--fail-on-llm` 语义**:dim-0 note 永不 gate(`score.Deterministic`、`report.HasAtLeast` 不动)
- **不改分数**:坏夹具仍是 100/100(这个 artifact 没被读,所以没有发现;让它扣分是另一个契约)
- **不改 `PARSE-000` 的严重度**(仍是 low):改了 JSON 就变了
- **不改其余三档的结论句**(Watch / Elevated / Critical):它们不是"安全"的断言
- **不修 Checked 那句的两个措辞缺陷,它们合成一份独立 proposal,本条合入后另开**(人定,2026-10-09):
  (a) `check <单个脚本>` / `check <普通目录>` 一边报发现一边说 "Nothing was found to check under this root."(清单计数
  不含 file / directory 类 artifact,和 dim-0 note 无关);
  (b) 读不了的 `settings.json`(扫描级 `IO-000`)头条已对冲,但 Checked 那句仍是 "Nothing was found to check"(未决问题 8)。
  两者都是"Checked 那句只从采集器抽出的计数推",修在同一处。detect 自己放在扫描级的、关于已加载内容的覆盖 note
  不对冲头条(见「不能说什么」)也归那一份
- **不动按设计不读的那些扫描级 note 的展示**:"顶层条目没读"的 `COV-000`、`LLM-002` 仍在 "Not checked" 那行里,条数、
  最高严重度、规则 ID 照旧;收窄只改它们**不**影响头条
- **不碰 Downloads 一节**(`cmd/aguard/inbox.go` 已经把条目里 artifact 的 dim-0 分进 `it.Notes`)和**闸门**(`internal/gate`)
- `go.mod` / `go.sum` 不动,不加依赖

## 不能说什么

- 不说"坏掉的配置现在会让 `check` 失败":修的是**看得见**,不是**挡得住**。报告显示这条 note 之后,`check` 在默认
  `--fail-on high` 下(以及任何 `--fail-on`)仍退出 0,CI 照样绿
- 不说"坏配置现在扣分":仍是 100/100,头条说的是覆盖不全,不是有风险
- 不说"所有读不全的情形现在都会点名文件":Summary 里点名的只有 artifact 自带 note 的那几项;扫描级 note 仍折在
  "Not checked" 那一行,文件在 `--verbose` / markdown / HTML 里
- 不说"终端报告以前漏掉了扫描级 note":扫描级的一直在 "Not checked" 那行里;漏的只是挂在 artifact 上的那种
- 不说 `--json` / SARIF 以前不全:它们一直带着这条
- 不说"头条和 'Not checked' 那行说的是同一个集合"(人定收窄):头条只看 Claude Code 加载的东西有没有读全;
  "Not checked — … coverage is incomplete" 那行仍数全部覆盖 note。所以 Low 档报告可以同时出现 `looks safe` 和那一行
  (例如只有"顶层条目没读"的 `COV-000`,真机上几乎总是这样)
- 不说"凡是加载内容没读全都会对冲头条":detect 把它自己的覆盖 note(artifact 里读不了的条目、超大文件、hook 脚本没跟进)
  放在**扫描级**、规则 ID 是 `COV-000`,按人定的集合(挂在 artifact 上的 note + 任何位置的 `IO-000` / `PARSE-000`)不动头条,
  只进 "Not checked"

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 坏 / 好两个夹具的端到端测试、渲染器单测、头条测试,跑红;改正一条把缺陷钉成合格的旧断言;golden 从本仓 `main` 录 | `report, cmd: tests — a settings.json that does not parse renders as "looks safe" with no note in any human report (P-013)` |
| 2 | 三个给人读的渲染器把 artifact 自带的 dim-0 note 和扫描级 note 放进同一条通道;HTML 的单条证据 note 显示文件 | `report: an artifact's own coverage note reaches the terminal, markdown and HTML reports (P-013)` |
| 3 | Summary:有覆盖 note 时 Low 档不说 looks safe;Checked 句点名没检查全的项 | `report: the summary stops calling a scan safe when coverage is incomplete, and names what was not fully checked (P-013)` |
| 4 | `.claude/rules/report.md`、spec §9、README 与 architecture 两个对子各一句 | `docs: report rules, spec §9 and the README and architecture pairs say artifact-level notes render and the headline follows coverage (P-013)` |
| 5 | 人定收窄:只带"顶层条目没读"的 `COV-000` / 只带 `LLM-002` 的结果仍该说 looks safe,跑红;读不了的 `settings.json` 仍该对冲(反向) | `report, cmd: tests — an unowned top-level entry or the judge's privacy notice takes "looks safe" away from the headline (P-013)` |
| 6 | `coverageVerdict` 收窄:集合 = 挂在 artifact 上的覆盖 note + 任何位置的 `IO-000` / `PARSE-000`,只在这一处定义,按规则 ID 和挂载位置选 | `report: only what Claude Code loads and did not fully read takes "looks safe" away; unowned entries and the privacy notice stay in Not checked (P-013)` |
| 7 | `.claude/rules/report.md`、spec §9、README 与 architecture 对子按收窄改写 | `docs: report rules, spec §9 and the README and architecture pairs say only unread loaded content hedges the headline (P-013)` |
| 8 | 本文件、索引 | `proposals: P-013 (P-013)` |

W2–W4 是旧仓第一轮的宽集合,W5–W7 是人定的收窄;两段按原顺序各自成提交,一个 W 一个提交。

## 未决问题

1. **artifact 自带的 note 在哪一层并进去?**
   **建议**:在 `internal/report` 里给三个人读渲染器一个共同的取数函数(扫描级 note 在前,各 artifact 自带的按 artifact 顺序在后),
   数据不挪。挪数据(在 `analyze` 里把它们搬进 `ScanResult.Notes`)会改 JSON 字节和 SARIF 的 artifact 归属,而这两份本来就是对的;
   Downloads 一节已经这么做(`checkCandidate` 在组条目时把 dim-0 分进条目 notes),是同一个做法。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
2. **覆盖不全时头条说什么?**
   **建议**:Low 档的 `Your Claude Code setup looks safe.` 在**有任何覆盖 note** 时换成 `Low risk in what was read, but coverage is incomplete.`,
   计数子句照旧。"任何覆盖 note"就是 "Not checked — N coverage note(s) … coverage is incomplete" 那一行数的**同一个集合**
   (`splitNotes` 的覆盖那半);压制类 note 不算(它们在 Summary 里本来就有自己的一行)。其余三档不变。
   **否决的备选**:只在 artifact 自带 note 时改 —— 同一个 `settings.json` 读不了(`IO-000`,扫描级)和解析不了
   (`PARSE-000`,artifact 级)会得到相反的头条。**代价**:真机的 `~/.claude` 几乎总带一条"顶层目录没读"的 `COV-000`,
   落在 Low 档的真机报告头条会一直是这句;空 root 也是。
   **已决(2026-10-09)**:按建议(旧仓)。
   **已决(2026-10-09,人)**:收窄。只有 **Claude Code 实际加载的东西没读全**时才把 Low 档的 "looks safe" 换成对冲句:
   `PARSE-000`、`IO-000`(扫描级的也算,例如读不了的 `settings.json`),以及挂在 artifact 上的覆盖 note(`COV-000` / `SCOPE-001`
   挂在 artifact 上时;`SCOPE-001` 今天是维度 9 的计分发现,不是 note,将来若以维度 0 挂在 artifact 上同样触发)。扫描级的
   "顶层条目没读" `COV-000`(无人认领的非配置条目,按设计不读)和 `LLM-002`(隐私告知)**不改头条**,仍照原样出现在
   "Not checked" 那行。理由:真机上那条 `COV-000` 几乎总在,宽规则会让对冲句出现在几乎每一份 Low 档报告上,它就不再有
   任何意义。实现:集合只在 `coverageVerdict` 里定义,按规则 ID 和挂载位置(artifact / 扫描)选,不按标题匹配;上面
   "否决的备选"里担心的 `IO-000` / `PARSE-000` 头条不一致,因为 `IO-000` 在集合里而不存在。W5–W7。
3. **"Nothing was found to check under this root." 怎么办?**
   **建议**:那句是清单的派生。artifact 自带覆盖 note 意味着"找到了、没检查全",所以在那句里点名:`Not fully checked: <短路径> [<规则 ID>]`,
   最多 3 项、余下计数;有清单计数时接在 `Checked …` 之后。清单为空且没有这种 artifact 时原句不变。扫描级 note 不在这里点名 ——
   它们不是清单里的项,下面那行已经数了它们。这也是默认视图里**文件名**出现的地方。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
4. **默认终端视图要不要把 artifact 自带的 note 单独展开?**
   **建议**:不。和扫描级 note 一起折进同一行(条数、最高严重度、规则 ID 都把它算进去);文件名已经在 Summary 里;全文是 `--verbose` 的事。
   单列一块等于发明第二条通道。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
5. **HTML 的 "Not checked" 区块要不要显示文件?**
   **建议**:要 —— 只有一条证据的 note 显示 `— <文件>`,和 markdown、`--verbose` 已有的规则相同(`len(Evidence)==1`)。否则 HTML 里
   这条只剩标题 "Parse failed, artifact not fully covered",说不出是哪个文件。副作用:HTML 里单条证据的扫描级 note 也多出文件名,是补齐
   三个渲染器,不是新规则。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
6. **退出码 / `--fail-on`?**
   **建议**:不动。dim-0 永不 gate(不变量 #4,`score.Deterministic`);修完后报告显示 note,默认阈值下仍退出 0,写进「不能说什么」。
   让坏配置挡住闸门要改 `--fail-on` 的契约,不在本条。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
7. **一条既有测试把缺陷钉成了合格,怎么处理?** `TestText_AggregatesAndLabels`(`internal/report/text_test.go`)的样例里有一条
   artifact 自带的 `COV-000`,断言是"整个输出里没有 `COV-000`"—— 本意是"不作为风险行出现",写法却同时断言了"根本不显示"。
   **建议**:改成"Findings 一节里没有、Not checked 那行里有",在 W1 里改并披露。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
8. **读不了的 `settings.json`(`IO-000`,扫描级)那一半?** 修完后它的头条也说 `coverage is incomplete`,但 Checked 那句仍是
   "Nothing was found to check under this root." —— 它没有 artifact,不在"找到了、没检查全"的清单里。
   **建议**:不在本条,和「不做什么」里 `check <单个脚本>` 那条一起另开:两者都是"Checked 那句只从采集器抽出的计数推",
   修在同一处;本条的契约("artifact 自带的 note 有人渲染、头条不再说 looks safe")已满足。
   **已决(2026-10-09,人)**:两个措辞缺陷(这一条,和 `check <单个脚本>` / `check <普通目录>` 报发现时仍说 "Nothing was found
   to check")合成**一份**独立 proposal,本条合入后另开;本条不修。收窄后 `IO-000` 仍在头条的集合里,头条那半不变。

## 完成

```
合入:PR #28(2026-10-09;sha 用 git log --grep P-013 找)
发布:待发
证据:TestBrokenSettingsIsNotReportedSafe(cmd/aguard/artifact_notes_test.go);W1 在本仓 main 的渲染器上红:scanEnv 与 checkTarget 两路,终端默认 / --verbose / markdown / HTML 四个都 "does not name the parse failure [PARSE-000]",且都说 "looks safe" 和 "Nothing was found to check"(24 处)→ W2 后 PARSE-000 进了四个渲染器 → W3 后四个全绿:Summary 是 "Low risk in what was read, but coverage is incomplete. No findings." + "Not fully checked: …/.claude/settings.json [PARSE-000]."
证据:TestCheckBrokenSettingsCLI(同文件);W1 红在 stdout(没有 PARSE-000、有 looks safe)→ W3 后绿;退出码修前修后都是 0
证据:TestArtifactNoteReachesEveryHumanRenderer(internal/report/artifact_notes_test.go);W1 红:四个渲染器都 "does not show the artifact's own note" → W2 后只剩终端默认红(文件名不在默认视图、没有 U+FFFD)→ W3 后绿;四个输出里 U+202E 与 ESC 都是 0 处,U+FFFD 在
证据:TestSummaryDoesNotCallIncompleteCoverageSafe(同文件);W1 红 16 处(扫描级 IO-000 / artifact 级 PARSE-000 × 4 个渲染器 × 2 条断言)→ W3 后绿;"no note" 与 "trust decision only" 两行 W1 起就绿,之后不改一字仍绿
证据:人定收窄 —— 同一测试新增三行;W5 红 8 处("unowned top-level entries only" 与 "judge privacy notice only" × 4 个渲染器,looks safe = false, want true)→ W6 后绿;"scan-level PARSE-000" 一行前后都绿;原有四行 W5 / W6 未改一字,仍绿。TestCoverageVerdict(W3 自己加的)随 coverageVerdict 改为接收整个结果而改了入参,每行期望的句子不变
证据:TestUnownedEntriesKeepTheHeadline(cmd/aguard/artifact_notes_test.go);W5 红:四个渲染器都 "an unowned top-level entry changed the headline" → W6 后绿,四个都说 Your Claude Code setup looks safe.,且 Not checked 行 / Scan warnings / ## Not checked / notchecked 区块仍在
证据:反向断言 TestUnreadableSettingsHedgesTheHeadline(同文件);W5 时就绿,W6 后不改一字仍绿;变异(coverageVerdict 的扫描级集合去掉 IO-000)→ 四个渲染器都红 "an unreadable settings.json must hedge the headline",还原后绿
证据:反向断言 TestCleanSettingsReportIsUnchanged(同文件);golden 在 W1 上用本仓 main 的渲染器现录(临时 dump 测试,未提交),与旧仓 golden 只差 markdown 末尾 rules 链接 blob/main ← blob/dev;W1 起绿,W2–W7 后不改一字仍绿(终端默认 = --verbose,markdown 逐字节)
证据:反向断言 TestBrokenSettingsMachineOutputUnchanged(同文件);W1 起绿,之后不改一字仍绿:JSON 里 PARSE-000 仍在 artifacts[0].findings、notes 为 [],SARIF 归属 hook:settings.json,overall 100,--fail-on high / low 都不触发
证据:反向断言 —— TestVerdictSentence、TestCheckedLine(internal/report/plain_test.go)未改、仍绿;text_test.go 只改了 TestText_AggregatesAndLabels 的一条断言(未决问题 7),W1 红 "must fold into the Not checked line, not vanish" → W2 后绿
证据:真机二进制 —— main(dec64ca)与本分支各编一个(同一 -ldflags version 串),7 个夹具(好、坏 settings.json、坏 ~/.claude.json、坏 installed_plugins.json、只带顶层条目的、空 root、chmod 000 的 settings.json)× check --json(去 scanned_at)/ check --sarif 共 14 个文件 0 字节差;好夹具的终端默认 / --verbose / --md(去时间行)/ scan --html(去 meta 行)0 字节差;只带顶层条目的夹具和空 root 的终端默认 / --verbose / --md / scan 输出 0 字节差,两者 HTML 只多出单条证据 note 的文件名(未决问题 5)
证据:真机二进制 —— 坏 settings.json、坏 ~/.claude.json、坏 installed_plugins.json 三个 withParseError 调用点由 "looks safe … Nothing was found to check" 变为 "Low risk in what was read, but coverage is incomplete." + "Not fully checked: <短路径> [PARSE-000].",Not checked 行出现 [PARSE-000];markdown 多出 "## Not checked",HTML 多出 notchecked 区块并印出文件;chmod 000 的 settings.json 头条对冲,Checked 那句仍是 "Nothing was found to check"(不做什么 (b));7 个夹具退出码前后都是 0
证据:真机 ~/.claude(scan --inbox off,175 项,Elevated,605 行):main 与本分支输出 0 字节差 —— 不在 Low 档,也没有 artifact 自带的 note
证据:不做什么 —— git diff --stat origin/main -- internal/collect internal/detect internal/score internal/model internal/report/sarif.go internal/report/sanitize.go cmd/aguard/main.go cmd/aguard/inbox.go internal/gate go.mod go.sum 为空
证据:移植 —— 7 个补丁在本仓 main 上 git am -3 全部无冲突;本仓 internal/report 与旧仓导出基点只差一处(markdown 里 rules 链接的分支名 blob/main ← blob/dev,已体现在重录的 golden 里),v0.16–v0.18 没有再动 internal/report(git log 2b4c2a7..origin/main -- internal/report 为空)
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5
```
