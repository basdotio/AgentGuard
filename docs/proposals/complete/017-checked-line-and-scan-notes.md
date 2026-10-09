<!-- SPDX-License-Identifier: MIT -->
# 017 — 摘要自相矛盾:check 一个文件一边列发现一边说 "Nothing was found to check";已加载内容里没读到的部分不动 "looks safe"

- **来源**:P-013 记下的后续(它的「不做什么」(a)(b)、「不能说什么」最后一条、未决问题 8;人定 2026-10-09 合成一份,P-013 合入后另开)
- **依赖**:P-013(已合入 `main`)
- **分支**:`p/017-checked-line-and-scan-notes`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

报告的 Summary 是不写代码的读者真正会读的那一半,它有两句:头条(等级档 + 计数派生的一句话)和 Checked 那句(检查了什么)。
在本仓 `main`(`fd28344`)编出的二进制上实测,两句都会和**同一份报告**下面列出的东西打架。夹具全部现搭在临时目录里。

**1. Checked 那句只从采集器的清单计数(`EnvSummary`)推,而有几类被扫过的东西不在清单里。** 于是:

| 目标 | 报告里同时出现的 | Summary 的第二句 |
|---|---|---|
| `check install.sh`(`curl … \| bash`) | `EXEC-001` high,"There are problems you should fix…" | `Nothing was found to check under this root.` |
| `check hello.sh`(干净) | "Your Claude Code setup looks safe." | `Nothing was found to check under this root.` |
| `check tool/`(普通目录,带同一个 `install.sh`) | `EXEC-001` high | `Nothing was found to check under this root.` |
| `scan --root` 一个只有 `CLAUDE.md` 的 `.claude`(带 `curl … \| bash`) | `EXEC-001`,69/100 Elevated | `Nothing was found to check under this root.` |
| `scan --root` 一个只有 `settings.json`、里面只有 `env` 块的 `.claude` | 一个 `permission:settings env` artifact | `Nothing was found to check under this root.` |
| `scan --root` 一个 `settings.json` 是 `chmod 000` 的 `.claude` | 头条已对冲(P-013),Not checked 里有 `IO-000` 指着 `settings.json` | `Nothing was found to check under this root.` |

终端默认、`--verbose`、`--md`、HTML(`scan --html`)四个渲染器一字不差。原因:`check <单文件>` 产出 `instruction`(或 `command`)类
artifact、`check <普通目录>` 产出 `directory` 类 artifact,`CollectTarget` 给这两种的 `EnvSummary` 都是空的;root 里的
`CLAUDE.md` 是 `instruction` 类,`settings.json` 的 `env` 块是不计条数的 `permission` 类 —— 清单里都没有它们,于是
`checkedParts` 什么都数不出来,`checkedWithGaps` 落到 "Nothing was found"。读不了的 `settings.json` 是扫描级 `IO-000`、
没有 artifact,P-013 的"找到了、没检查全"只点名挂在 artifact 上的 note,所以它也落到同一句。

一句"什么都没查"紧挨着它自己查出来的 high,读者只能二选一地不信其中一句。

**2. 头条只在"挂在 artifact 上的覆盖 note,或任何位置的 `IO-000` / `PARSE-000`"时对冲(P-013 人定的集合),而 detect 关于已加载
内容的覆盖 note 全部是扫描级的 `COV-000`**(`detect.Engine.Run` 把每个 artifact 的维度 0 结果收进扫描级 notes)。实测:

| 夹具 | 没读到的 | 分数 / 头条 |
|---|---|---|
| skill 的 `SKILL.md` 写 "Run sh sub/inner.sh",`sub/` 是 `chmod 0111`,里面是 `curl … \| bash` | `COV-000` medium "Entries in this artifact could not be read" | 100 / "Your Claude Code setup looks safe." |
| skill 里一个 1.1 MB 的 `big.sh`,末尾是 `curl … \| bash` | `COV-000` "File too large, content scan skipped" | 100 / "looks safe" |
| skill 的 `SKILL.md` 写 "Run node node_modules/dep/setup.js" | `SUP-004` medium(指向扫描不读的目录)+ `COV-000` "Third-party / VCS trees not read" | 88 / "looks safe. 1 finding needs a look" |
| `settings.json` 注册的 hook 跑 `sh ~/.claude/hooks/missing.sh` | `COV-000` "Hook script not followed" | 100 / "looks safe" |
| `.claude/rules` 是指向 root 外的软链(dotfiles 常见布局) | `COV-000` medium "Entries resolve outside the scanned root, not read"(Why:"They are loaded by Claude Code but were NOT scanned") | 100 / "looks safe" + "Nothing was found to check" |

前两行就是 W-001 / 超大文件这两种规避形状:payload 放在扫描器读不到的地方,报告的头条照样说安全,只有折起来的
"Not checked — 1 coverage note(s), highest medium [COV-000]" 那一行知道。四个渲染器的头条相同。不变量 #5 要求的是"缺口不静默",
这些 note 确实进了 Not checked;但 P-013 已经把"looks safe"定义成"Claude Code 加载的东西都读全了"的断言,而上面每一行都是
加载的东西没读全 —— 头条说错了话。

`collect` 也有几条同类的扫描级 `COV-000`(加载命名空间里解析不了的条目、`rules/` 等解析到 root 外、目录深度上限、托管策略
指令文件、隔离区越界、`@import` 越界 / 拒读凭据 / 深度上限、桌面版会话缓存上限),同样不动头条。

## 初步方向

只改 `internal/report` 的白话层(`plain.go`)和三个人读渲染器调用它的地方,从 `ScanResult` 里已有的数据派生,不加阈值、
不挪数据:Checked 那句在清单数不出东西时改数实际扫过的 artifact,并把扫描级 `IO-000` / `PARSE-000` 也点名成"没检查全";
头条的集合扩到"已加载内容没读全"的扫描级覆盖 note,按设计不读的几条照旧不动头条。JSON / SARIF、分数、退出码不动。

## 完成的判据

夹具都在 `t.TempDir()` 里现搭,走真实流水线(`checkTarget` / `scanEnv`),四个给人读的渲染器(终端默认、`--verbose`、markdown、HTML)
各取 Summary 一段(`summaryOf`,P-013 的同一个切法)。

- [x] `TestCheckedLineCountsWhatWasScanned`(`cmd/aguard/checked_line_test.go`,新):`check <带 curl|bash 的 install.sh>`、
  `check <干净的 hello.sh>`、`check <commands/deploy.md>`、`check <普通目录>`、`scan` 只有 `CLAUDE.md` 的 root、`scan` 只有 `env` 块的
  `settings.json` 的 root —— 四个渲染器的 Summary 都**不含** `Nothing was found to check`,分别含 `Checked 1 file.` / `Checked 1 file.` /
  `Checked 1 command.` / `Checked 1 directory.` / `Checked 1 file.` / `Checked 1 settings block.`。今天红:6 × 4 处都是 "Nothing was found"
- [x] `TestUnreadableSettingsIsNamedNotFullyChecked`(同文件,新):`chmod 000` 的 `settings.json`(扫描级 `IO-000`、没有 artifact)→ 四个
  渲染器的 Summary 含 `Not fully checked:`、`settings.json`、`IO-000`,不含 `Nothing was found to check`。今天红。以 root 运行读得到时 skip
- [x] `TestLoadedContentLeftUnreadHedgesTheHeadline`(同文件,新):五个 Low 档夹具 —— skill 里 `chmod 0111` 的子目录(以 root 运行时 skip)、
  skill 里超过 1 MiB 的脚本、`SKILL.md` 指向 `node_modules/` 里的文件(`SUP-004`)、hook 跑一个不存在的脚本、`rules/` 是指向 root 外的软链
  —— 四个渲染器的 Summary 都含 `Low risk in what was read, but coverage is incomplete.`、不含 `looks safe`。今天红:5 × 4 处都是 "looks safe"
- [x] `TestScanLevelNotesThatHedge`(`internal/report/scan_gaps_test.go`,新):同一个干净的 Low 档结果各加一条扫描级说明。对冲:`IO-000`、
  `PARSE-000`、detect 与 collect 关于已加载内容的每一种 `COV-000`(读不了的条目、超大文件、非常规文件、hook 脚本没跟进、授权脚本没跟进、
  解析到 root 外、托管策略文件)、一个带 `SUP-004` 的 artifact。**不**对冲(反向):按设计不读的四条(无人认领的顶层条目、空 root、
  第三方 / VCS 树、hook 脚本记在插件名下)、`LLM-000`、`LLM-002`、`LLM-005`、`GATE-001`、`REP-GOOD`、`IGN-000`。今天红在对冲那一半
- [x] 反向断言 `TestDeliberateSkipsKeepTheHeadline`(`cmd/aguard/checked_line_test.go`,新,今天就绿):真实流水线,skill 里没人指向的
  `node_modules/`、插件的 hook 脚本在插件树里找到(`Hook script attributed to its plugin`)、空 root、干净的 skill —— 四个渲染器都仍是
  `Your Claude Code setup looks safe.`;前两个的说明仍在 Not checked 里(P-013 的 `notCheckedMarker`);干净 skill 仍是 `Checked 1 skill.`,
  空 root 仍是 `Nothing was found to check under this root.`。修完不改一字仍绿
- [x] 反向断言:P-013 的 `TestCleanSettingsReportIsUnchanged`(干净配置终端默认 / `--verbose` / markdown 逐字节 golden)、
  `TestUnownedEntriesKeepTheHeadline`、`TestUnreadableSettingsHedgesTheHeadline`、`TestSummaryDoesNotCallIncompleteCoverageSafe`、
  `TestCoverageVerdict`、`TestCheckedWithGaps`,以及 `TestCheckedLine`、`TestVerdictSentence` 不改一字仍绿(`git diff origin/main -- <这些测试文件>` 为空)
- [x] 真机二进制:本仓 `main` 与本分支各编一个,上面每个夹具的 `--json`(去掉 `scanned_at`)和 `--sarif` 输出 `diff` 为 0 字节(数据不动)
- [x] 真机 `~/.claude`(`scan --inbox off`):与 `main` 的终端输出只差 Checked 那一行(扫描级 `PARSE-000` 被点名),头条不变(不在 Low 档)
- [x] `make verify` 绿;`go version` 不切换工具链,`go.mod` 第二行仍是 `go 1.23.5`

## 不做什么

- **不挪数据**:`ScanResult`、`model`、JSON / SARIF 的字节不动。detect 照旧把它的覆盖 note 放在扫描级,coalescer 照旧合并;
  collect 照旧产出同样的 note。`collect` / `detect` 只把**四条已有标题**从包内常量 / 字面量改成导出常量(字符串一字不改),此外一行不动
- **不改分数、退出码、`--fail-on` / `--fail-on-llm`**:维度 0 永不 gate(`score.Deterministic`、`report.HasAtLeast` 不动);`SUP-004`
  的严重度不动
- **不改 Not checked 那一行**:条数、最高严重度、规则 ID 照旧数全部覆盖 note
- **不改其余三档的结论句**(Watch / Elevated / Critical),也不改计数子句
- **清单计数不为空时 Checked 那句一字不变**:不把 `CLAUDE.md` 之类清单不数的东西补进有计数的那一行(那会改掉几乎每台真机的那一行,
  而那一行今天没有说错)
- **不碰 Downloads 一节**(`inboxAdvice` 不看条目自己的覆盖说明,是另一件事,见「完成」的后续)、**不碰闸门**(`internal/gate`)
- **不修 collect 的空 root 说明**:root 里唯一的加载内容解析到 root 外时,collect 仍出 `Nothing to audit under this root`,Checked 那句
  跟着它说 "Nothing was found to check"(头条会对冲),见未决问题 7
- `go.mod` / `go.sum` 不动,不加依赖

## 不能说什么

- 不说"读不全的内容现在会让 `check` 失败":修的是摘要的措辞。分数、退出码、`--fail-on` 都没变,一个 `chmod 0111` 子目录里藏着
  `curl | bash` 的 skill 仍是 100/100、退出 0 —— 报告现在**说**它没读全,不是**挡住**它
- 不说"凡是没读的都会对冲头条":按设计不读的四条(无人认领的顶层条目、空 root、第三方 / VCS 树、hook 脚本记在插件名下)不动头条;
  判官的 `LLM-*`、闸门的 `GATE-001`、压制类 note 也不动。头条和 Not checked 那一行仍**不是**同一个集合
- 不说"`node_modules/` 现在会对冲头条":没人指向它时不会;只有 artifact 把 agent 指进去(`SUP-004`)才会
- 不说"Summary 现在点名所有没读全的东西":Checked 那句点名的是挂在 artifact 上的覆盖 note 和扫描级 `IO-000` / `PARSE-000`;detect 与
  collect 的扫描级 `COV-000` 只让头条对冲,文件在 Not checked 里
- 不说"'Nothing was found to check' 不会再出现":空 root 照旧;root 里唯一的加载内容解析到 root 外时也照旧(未决问题 7)
- 不说"清单现在列出 `CLAUDE.md`":只有清单什么都没数到时,Checked 那句才改数扫过的 artifact

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 端到端与单元测试,跑红;反向断言今天就绿 | `report, cmd: tests — a checked file or directory reads "Nothing was found to check", and loaded content left unread still reads "looks safe" (P-017)` |
| 2 | Checked 那句:清单数不出东西时数扫过的 artifact;扫描级 `IO-000` / `PARSE-000` 点名成没检查全 | `report: the Checked line counts what was scanned when the inventory counts nothing, and names a file that could not be read or parsed (P-017)` |
| 3 | collect / detect 导出四条按设计不读的说明的标题常量,字符串不变 | `collect, detect: export the titles of the four notes that disclose a deliberate skip, so the report can tell them from a gap (P-017)` |
| 4 | 头条集合:扫描级 `COV-000` 除四条按设计不读的都对冲;带 `SUP-004` 的 artifact 对冲 | `report: loaded content the scan did not read takes "looks safe" away wherever its note sits; the four deliberate skips do not (P-017)` |
| 5 | `.claude/rules/report.md`、spec §9、README 与 architecture 两个对子 | `docs: report rules, spec §9 and the README and architecture pairs say what the Checked line counts and which scan-level notes hedge the headline (P-017)` |
| 6 | W4 的收尾:`IO-000` / `PARSE-000` 的判断只留一个谓词(头条和 Checked 那句共用);四条例外从可变 map 改成 switch,行为不变 | `report: one predicate for a file found and not read, shared by the headline and the Checked line; the deliberate skips are a switch, not a mutable map (P-017)` |
| 7 | 本文件、索引 | `proposals: P-017 (P-017)` |

W6 是 W4、W5 之后自审时补的:`IO-000` / `PARSE-000` 两个字面量在 `unreadLoaded` 和 `scanGaps` 里各写了一遍,正是本仓库反复拒绝的
"两份条件靠注释同步"。只是重构,W1 的测试与变异检查在它前后都同样通过。

## 未决问题

1. **清单什么都没数到时,Checked 那句说什么?**
   **建议**:数实际扫过的 artifact(不算 P-013 的"没检查全"项),按种类各用一个固定名词:`instruction` → file、`directory` → directory、
   `permission` → settings block、`quarantined` → quarantined item,其余沿用清单自己的名词(skill、hook、command…);种类顺序固定,
   与 artifact 顺序无关。只在清单计数为空时这么做,所以每一份清单里有数的报告那一行逐字节不变。
   **否决的备选**:(a) 总是把清单不数的种类补上(`CLAUDE.md` 旁边有 skill 时也写 "1 file")—— 几乎每台真机都有 `CLAUDE.md`,
   会改掉每台真机的那一行,而那一行今天并没说错;(b) 点名文件 —— 每个渲染器要各自引用一个被扫目录给的名字,而点名是下面发现列表的事;
   (c) 一律写 "N items" —— `check install.sh` 说 "Checked 1 item." 不如 "Checked 1 file." 让人看得懂。
   **已决(2026-10-09)**:按建议。
2. **扫描级 `IO-000` / `PARSE-000` 要不要在 Checked 那句里点名?**
   **建议**:要,无条件,和 artifact 自带的 note 排在一起(artifact 的在前、扫描级的在后,同一个 3 项上限)。它们是 P-013 那个"孪生"论证的
   另一半:同一个 `settings.json` 解析不了(artifact 上的 `PARSE-000`)今天点名,读不了(扫描级 `IO-000`)却落到 "Nothing was found";
   collect 给这两种 note 的证据都是一个文件路径。**代价**:真机 `~/.claude` 有一条扫描级 `PARSE-000`(一个插件 `hooks.json` 里的 hook 条目没看懂),那份
   Elevated 报告的 Checked 那句会多出 `Not fully checked: hooks.json [PARSE-000].` —— 是真话,写进「完成」。(design 时这里误写成
   `settings.json`;实测是 `hooks.json`,collect 给这条 note 的证据只有文件的 base 名。)扫描级 `COV-000` **不**点名:
   detect 的那些说的是已经数进清单的项的一部分(skill 的一个子目录、插件里的一个大文件、hook 的第二段),证据路径是相对 artifact 的
   (读不了的子目录那条是 `.`),点出来是噪音;它们在 Not checked 里有自己的行。
   **已决(2026-10-09)**:按建议。
3. **哪些扫描级 `COV-000` 对冲头条?**
   **建议**:全部,**除了**四条按设计不读、由生产方以导出的标题常量标明的说明:无人认领的顶层条目(`collect.UnownedNoteTitle`,P-013 人定)、
   空 root(`collect.EmptyRootNoteTitle`:什么都没采到,没有"加载了却没读"的东西)、第三方 / VCS 树(`detect.GeneratedDirNoteTitle`:按设计
   不读,真机上就有;artifact 把 agent 指进去时 `SUP-004` 负责)、hook 脚本记在插件名下(`detect.HookOwnedNoteTitle`:内容**读了**,只是归属
   不全,真机上一条合并了 50 处)。以后新加的扫描级 `COV-000` 默认对冲 —— 错的方向是"少说一句安全",不是"多说"。
   **否决的备选**:(a) 正向列出对冲的标题(十几条常量)—— 以后新加的缺口默认不对冲,正是这次要修的形状;(b) 让 detect 把覆盖 note 挂到
   artifact 上 —— JSON 与 SARIF 的归属都变,还会拆掉 coalescer 的合并(一条变 50 行);(c) 按证据路径的形状猜(相对路径 = detect 的)——
   collect 也出相对路径(hook 条目没看懂那条 `PARSE-000` 的证据就是 `settings.json`),证据路径是给人看的,不是契约。
   **已决(2026-10-09)**:按建议。
4. **这和 P-013 写进 `report.md` 的"按规则 ID 和挂载位置选,不按标题匹配"冲突吗?**
   **建议**:那条防的是渲染器自己写一段标题字面量、生产方一改措辞集合就悄悄漂走。扫描级的 `COV-000` 之间,规则 ID、挂载位置、来源全都相同
   —— 数据里除了标题没有别的东西能把"用户自己的会话记录没读"和"skill 里一个子目录读不了"分开。用生产方**导出的常量**比较,是 detect 自己的
   coalescer 已经在用的做法(`generatedDirNoteTitle` 的注释:producer 和 coalescer 共用,两边不会漂);渲染器里仍然不出现任何标题字面量,
   改措辞的人改的是同一个常量。`report.md` 那一条改写成这个意思。
   **已决(2026-10-09)**:按建议。
5. **`SUP-004` 要不要对冲?** 它是计分发现,不是覆盖 note。
   **建议**:要。它是唯一一条意思就是"加载的东西没读"的计分规则(agent 被指进一个按名字不读的目录),而它旁边那条说明是按设计不读的
   第三方树、不对冲;不加它,"指进 `node_modules/`"这一种就仍说 looks safe。`HOOK-002`、`EXFIL-005` 不需要:它们各自带一条会对冲的 `COV-000`。
   按规则 ID 选,只看静态来源。
   **已决(2026-10-09)**:按建议。
6. **collect 的那几条扫描级 `COV-000`(解析不了的加载命名空间条目、解析到 root 外、深度上限、托管策略文件、隔离区越界、`@import` 三条、
   桌面版会话缓存上限)算不算在内?**
   **建议**:算。它们说的是同一件事(不少条的 Why 原文就是 "loaded by Claude Code but were NOT scanned"),由问题 3 的规则自然覆盖,
   不需要额外代码。托管策略文件在受管机器上总在,那里的 Low 档头条会一直对冲 —— 它每次会话最先加载、而这次扫描没读,对冲是真话。
   **已决(2026-10-09)**:按建议。
7. **root 里只有一个解析到 root 外的 `rules/` 软链时,Checked 那句仍是 "Nothing was found to check"?**
   **建议**:本条不改。那时 collect 一个 artifact 都没采到,它自己的 `Nothing to audit under this root` 说明照样出,Checked 那句与它一致;
   头条已经对冲。要改的是 collect 什么时候出空 root 说明,不在渲染器里,记为后续。
   **已决(2026-10-09)**:按建议。

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-017 找)
发布:待发
证据:TestCheckedLineCountsWhatWasScanned(cmd/aguard/checked_line_test.go);W1 在本仓 main 的渲染器上红:6 个子测试 × 终端默认 / --verbose / markdown / HTML 共 24 处都是 "Nothing was found to check"(check install.sh、check hello.sh、check commands/deploy.md、check 普通目录、只有 CLAUDE.md 的 root、只有 env 块的 settings.json)→ W2 后绿:Checked 1 file. / 1 file. / 1 command. / 1 directory. / 1 file. / 1 settings block.
证据:TestUnreadableSettingsIsNamedNotFullyChecked(同文件);W1 红:四个渲染器都缺 "Not fully checked:"、"settings.json"、"IO-000" 且说 "Nothing was found to check"(16 处)→ W2 后绿:"Not fully checked: …/.claude/settings.json [IO-000]."(markdown 是代码跨度)
证据:TestLoadedContentLeftUnreadHedgesTheHeadline(同文件);W1 红:5 个 Low 档夹具 × 4 个渲染器共 20 处说 "looks safe"(chmod 0111 子目录里的 curl|bash、1.1 MB 脚本末尾的 curl|bash、指进 node_modules 的 SUP-004、hook 跑不存在的脚本、指向 root 外的 rules/ 软链)→ W2 后仍红(只改了 Checked 那句)→ W4 后绿
证据:TestScanLevelNotesThatHedge(internal/report/scan_gaps_test.go);W1 红 10 行 × 4 个渲染器(8 种 detect / collect 的扫描级 COV-000 + 托管策略 + SUP-004),IO-000 / PARSE-000 两行与 10 行不对冲的(四条按设计不读、LLM-000/002/005、GATE-001、REP-GOOD、IGN-000)W1 起就绿 → W4 后全绿
证据:TestCheckedLineDerivesFromWhatWasScanned(同文件);W1 红 6 / 9 行(单个文件、各种不计数的种类按固定顺序、gap 不算"检查了"、扫描级 IO-000 接在计数后 / 单独出现、artifact 的 gap 排在扫描级前)→ W2 后绿;"扫描级 COV-000 不点名"、"清单有数时那一行不变"、"什么都没有"三行 W1 起就绿
证据:反向断言 TestDeliberateSkipsKeepTheHeadline(cmd/aguard/checked_line_test.go);W1 起绿,W2–W6 后不改一字仍绿:没人指向的 node_modules、在插件树里找到的 hook 脚本、空 root、干净 skill 四个渲染器都是 "Your Claude Code setup looks safe.",前两个的说明仍在 Not checked 里。变异:去掉 deliberateSkip 里的 HookOwnedNoteTitle → 插件 hook 子测试红、单元测试那一行红 ×4;去掉 GeneratedDirNoteTitle → node_modules 子测试红;还原后绿
证据:反向断言 —— git diff origin/main -- internal/report/artifact_notes_test.go internal/report/plain_test.go internal/report/text_test.go cmd/aguard/artifact_notes_test.go 为空:P-013 的 TestCleanSettingsReportIsUnchanged(golden 逐字节)、TestUnownedEntriesKeepTheHeadline、TestUnreadableSettingsHedgesTheHeadline、TestSummaryDoesNotCallIncompleteCoverageSafe、TestCoverageVerdict、TestCheckedWithGaps,以及 TestCheckedLine、TestVerdictSentence 不改一字仍绿
证据:真机二进制 —— main(fd28344)与本分支各编一个,19 个夹具 × (--json 去掉 scanned_at / tool_version,--sarif 版本串归一)共 38 个文件 0 字节差;Summary 段(终端默认 / --verbose / --md / scan --html)与上面判据一致,没人指向的 node_modules 与干净 skill 两个夹具 0 字节差
证据:真机 ~/.claude(scan --inbox off,605 行,69/100 Elevated):main 与本分支只差第 7 行 —— Checked 那句多出 "Not fully checked: hooks.json [PARSE-000]."(一个插件 hooks.json 里的 hook 条目没看懂,未决问题 2);头条不变。scan --quiet 前后都是退出 0、0 行输出
证据:不做什么 —— git diff --stat origin/main -- internal/model internal/score internal/gate internal/inbox cmd/aguard/main.go cmd/aguard/inbox.go internal/report/sarif.go internal/report/sanitize.go go.mod go.sum 为空;internal/collect、internal/detect 只有 6 个文件 26+/15-:四个标题改成导出常量(字符串不变)和它们的注释、引用
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5
后续(没做,只记名):(1) collect 的空 root 说明在"唯一的加载内容解析到 root 外"时照样出,Checked 那句跟着说 Nothing was found to check(未决问题 7);(2) Downloads 一节的 inboxAdvice 不看条目自己的覆盖说明 —— 实测(本分支二进制):Downloads 里一个子目录 chmod 0111、里面是 curl|bash 的 skill 得 100/100,建议句是 "Nothing flagged by the static rules. Install it if you know where it came from.",那条 COV-000 印在下一行
```
