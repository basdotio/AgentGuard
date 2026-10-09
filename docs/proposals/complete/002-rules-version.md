<!-- SPDX-License-Identifier: MIT -->
# 002 — 报告不说自己是哪一版规则跑出来的,两份报告分不清是规则变了还是输入变了

- **来源**:新发现(2026-10-09):同一份内容两次扫描得分不同时,报告里没有任何东西说明规则表变没变;
  `rules_version` 是离线复算 `overall` 的前提。移植自旧仓 agent-guard 的 P-043(私有仓)
- **依赖**:无
- **分支**:`p/002-rules-version`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

一份 `scan --json` 关于"它是谁跑出来的"只有一个字段:`tool_version`,内容是构建时的版本号
(`make build` 注入 `git describe`,如 `v0.17.0-8-g4eb7716`、`-dirty`;`go install …@latest` 从 build info 取模块版本;
在 checkout 里直接 `go build` 则是 `dev`)。它标的是**一次提交**,不是**一套规则**,两个方向都错:

| 情形 | 实测(本仓 origin/main dec64ca,`git rev-list` / `git describe`) | 后果 |
|---|---|---|
| 版本号变了,规则没变 | v0.16.0 → v0.17.0:4 个提交,**0 个**碰 `internal/detect/` | 两份报告 `tool_version` 不同、发现不同时,读者没法排除"是规则改了";只能去翻 changelog |
| 规则变了,规则表文件没变 | v0.17.0 → v0.18.0:10 个提交里 `a882e42` 让 slash command 跑全套规则 —— 改的是 `detect.go` / `collect.go` 的角色判定,`rules.go` / `rules_data.go` 一字未动;只有 `4eb7716`(`EXEC-012`)碰了规则表 | 按"规则表文件动没动"判断会漏掉前者 |
| 规则变了,版本号看起来一样 | `git describe`:`a882e42`(slash command 跑全套规则)和 `5a60282`(信誉名单续期,不碰检测)都是 `v0.17.0-1-g…`;`881c417` 是 `v0.17.0-7-g881c417`,下一个提交 `4eb7716` 加了 `EXEC-012`,是 `v0.17.0-8-g4eb7716` | 只差一个后缀的两个版本号,读者没法知道中间隔没隔着一条规则 |
| 不经 `make` 的构建 | 本仓 checkout 里 `go build ./cmd/aguard` → `aguard dev (commit none, built unknown)`,`tool_version: "dev"` | 完全没有信息 |

`aguard version`、`docs/rules.md` 页眉也都不说规则表是哪一版。于是:

- `baselines/results/` 的成绩单要归因到规则集(`docs/corpus-benchmark.zh-CN.md` §6:"benchmark 头部要钉规则集哈希,否则数字无法归因";
  同文件的成绩单头部样例早就留了 `rules_version: <sha256 前 12 位>` 一栏),今天只能钉到提交;
- 一个用户拿两份报告来问"为什么这条发现没了",回答的第一步——"规则一样吗"——没有答案;
- 想对着产出一份报告的规则离线复算它的 `overall`,先得知道是哪套规则,今天报告里没有这个信息。

## 初步方向

在 `internal/detect` 里算 `RulesVersion()`:对 `builtinRules()` 按引擎遍历顺序,把每条的 ID、维度、严重度、
advisory 与几个 `*Only` 标志、正则源码(`re` / `except`)做规范化哈希,再折进一个手动递增的 `rulesEpoch`
(规则表之外的检测逻辑变了就加一)。正则源码字段是未导出的,所以只能在 `detect` 包里算,`hack/gen-rules` 拿不到。

它出现在三处:`ScanResult.RulesVersion`(`scan --json` / `check --json`)、`aguard version` 行末、`docs/rules.md` 页眉
(现有的漂移检查随之把"改了规则却没跑 `make docs`"拦下)。只交付 `rules_version` 这一个字段;
输入摘要(`inputs_digest`)和离线复核命令(`aguard verify`)不在本条。

## 设计

读过的代码(origin/main dec64ca):`internal/detect/rules.go`(`Rule` 的 13 个字段,`re` / `except` 未导出)、
`rules_data.go`(`builtinRules()` 是一个切片字面量,46 条,无重复 ID)、`detect.go:321`(每一行按 `e.rules` 的顺序逐条匹配、
命中就 append —— **同一行上的发现顺序就是规则顺序**)、`detect.go:225` `byID`(取第一个匹配)、`detect.go:695` `roleAllows`
(三个 `*Only` 标志和 `Dimension` 决定哪些规则在哪种文件上跑)、`model.go:457` `ScanResult`、`cmd/aguard/main.go:464`
(`ScanResult` 唯一的构造点,`scan` 与 `check` 都走 `analyze`)、`main.go:805`(version 行)、`cmd/aguard/buildinfo.go`
(`-ldflags` 没注入时从 build info 补 version / commit / date)、`cmd/aguard/version.go`(**已有一个 `versionLine`**,
是插件那一行的比较函数)、`hack/gen-rules/main.go`(页眉的计数块)、spec §5.1 / §8。

- **哈希什么**:按 `builtinRules()` 的顺序,每条规则的 `ID`、`Dimension`、`Severity`、`Advisory`、`HookOnly`、`ConnectorOnly`、
  `RawOnly`、`ScriptOnly`、`re.String()`、`except.String()`(没有就是空串),外加规则条数和一个未导出的 `rulesEpoch` 整数。
  每个字段长度前缀后写进 sha256(字段里可以有任何字节,分隔符不可靠),取前 12 位十六进制(corpus-benchmark 成绩单头部样例的定义)。
  **不哈希 `Title` / `Why` / `Ref`**:它们是给人读的解释,不决定一条发现响不响、多重。
- **`rulesEpoch`**:规则表之外的**确定性**检测代码(结构化/形状/外泄链检查、`permcheck`、词法层、角色门、读哪些文件、
  `collect` 的凭据 import 检查 `EXFIL-005`)一旦改变了"某个输入产出哪些确定性发现,或某条发现的 ID / 维度 / 严重度 / advisory",
  就在同一个提交里加一。上面「问题」表里的 `a882e42` 正是这一类:规则表一字未动,slash command 上的发现变了。
  **判官不在范围内**(人裁定):`rules_version` 的用途是离线复算 `overall`,判官只动 `overall_effective`。
  **今天报告只通过 `tool_version` 标识判官的代码**:`judge` 摘要只记跑没跑、跑了多少、连的哪个端点,
  不记模型、提示词版本、`samples` 或 `authority`;`llm` 配置不进报告。把"判官的规则映射"列进 epoch,
  等于让每个改 `internal/judge` 的提交都欠一次在 `internal/judge` 里看不见的 epoch。
  这条纪律写在常量的 doc 注释和 spec §5.1,外加 `.claude/rules/pipeline.md` 一行(它在 `internal/**`、`cmd/aguard/**` 加载,
  改表外检测代码的人才看得见;只写在常量旁边,改 `permcheck` 的人根本不会打开那个文件);
  **不写进 `.claude/rules/detect.md`**(198/200 行,`claude_rules_test.go` 卡 200)。
- **新字段必须表态,表态要被核实**:`Rule` 以后加一个字段,`TestRulesVersion_EveryRuleFieldIsDecided` 用反射逼它要么进哈希、
  要么进"只是文字"清单,**再逐字段改一次**:进哈希的改了版本号必须变,只是文字的必须不变。
  否则一个新的行为标志(比如再来一个 `DocOnly`)会悄悄不进版本号 —— 只查清单的话,登记了不哈希也是绿的。
- **纯函数**:`rulesVersion(rules []Rule, epoch int) string` 供测试变异;导出的 `RulesVersion()` 用 `sync.OnceValue` 只算一次
  (闸门每次加载 skill 都跑一次 `analyze`,不必每次重编译 46 个正则)。
- **三个出口**:`ScanResult.RulesVersion`(`json:"rules_version"`,紧挨 `tool_version`,**总是出现** —— 扫描总有规则表;旧报告没有这个键);
  `aguard version` 行末追加 ` · rules=<v>`,**`$2` 仍是版本号**(`.github/workflows/release.yml:60` 用 `awk '{print $2}'`,
  `baselines/adapter/aguard` 取首行整行当 `tool_version`,今后自然带上 `rules=`);`docs/rules.md` 页眉。
  `InboxReport` 不带 `tool_version`,它嵌在 `ScanResult` 里,同一个字段管它,不另加。
- **version 行的函数叫 `binaryVersionLine`**:本仓 `version.go` 已有 `versionLine(collect.PluginInstall, string)`(插件那一行),
  新函数换个名字,不动旧函数。`version` / `commit` / `date` 三个变量照旧由 `-ldflags` 注入、没注入时由 `buildinfo.go` 的
  `init()` 补齐,`binaryVersionLine` 只负责排版 —— 两种来源的值都不含空格,`$2` 契约不受影响。
- **页眉进了 rules.md,漂移检查的覆盖面随之变大**:以前只改正则不改标题,`rules.md` 一个字节不变;
  现在页眉的版本号会变,`make verify` / CI 的漂移步要求重跑 `make docs`。这是有意的,不是副作用。

## 完成的判据

- [ ] `TestRulesVersion_MovesWithWhatDecidesAFinding`(`internal/detect/rules_version_test.go`,新):在 `builtinRules()` 的副本上
  逐项变异 —— 改一条的正则、加/去 `except`、改严重度、改维度、改 ID、翻 `Advisory` 和四个 `*Only`、交换两条的顺序、删一条、`epoch + 1` ——
  每一项都让 `rulesVersion` 变。今天没有这个函数,编译即红
- [ ] `TestRulesVersion_IsStable`(同文件,新):`RulesVersion()` 两次调用相等,是 12 位小写十六进制,等于 `rulesVersion(builtinRules(), rulesEpoch)`
- [ ] **反向断言** `TestRulesVersion_IgnoresProse`(同文件,新):把**每一条**规则的 `Title`、`Why`、`Ref` 全换掉 → 版本号不变
- [ ] `TestRulesVersion_EveryRuleFieldIsDecided`(同文件,新):`Rule` 的每个字段都在"进哈希"或"只是文字"两张清单之一里,
  **并且**每个字段在一条规则的副本上被改一次(导出字段用反射:bool 取反、整数 +1、string / `Severity` 追加一个字节;`re` / `except`
  反射设不了,显式变异)—— 进哈希的必须让版本号变,只是文字的必须不变。**变异**:把 `Title` 挪进哈希清单,或给 `Rule` 加一个
  `DocOnly bool` 并只登记不哈希 → 红;只查清单的版本在两种变异下都绿
- [ ] `TestRulesEpoch_ScopeIsDeterministicOnly`(同文件,新):`rulesEpoch` 的 doc 注释写明"只覆盖确定性检测"、
  "判官在 rules version 之外",且不把 `clampSeverity`、`LLM-007`、"the judge's rule mapping" 列为 epoch 覆盖的代码
- [ ] `TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion`(`hack/gen-rules/main_test.go`,新):页眉写明
  "covers deterministic detection only"、"**Outside `rules_version` entirely:** every `LLM-` ID",以及"today a report identifies the judge's code
  only through `tool_version`";不出现 "how it ran"(报告里没有字段说判官怎么跑的)
- [ ] `TestRulesDocHeaderSaysWhatTheHashCovers`(同文件,新):页眉把报告能带的每个 ID 分进**恰好一类**,每类只说一次 ——
  **Hashed**(N 条引擎规则)、**Covered only by the epoch**(结构化、权限、非 `LLM-` 的 scan note、`GATE-001`)、**Outside `rules_version`**
  (每个 `LLM-` ID);各类计数取自生成器自己的表,不手写。测试从表里建分类,页面上每个 ID 必须恰好落一类(`GATE-000` 除外,它不进任何报告);
  不出现 "It hashes what decides a finding"、"scan-note and gate entries"、"notes included"、`GATE-000`。**变异**:`gateNotes` 加一条
  `GATE-002`,或把一个 `LLM-` ID 放进 `structural` → 红
- [ ] `TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion`(`internal/detect/rules_version_docs_test.go`,新):`rulesEpoch` 注释、
  `ScanResult.RulesVersion` 注释、architecture 对子、spec §5.1 与 §8 六段都说"只通过 `tool_version`",都不说 "how it ran" / "怎么跑的" / "Judge 说明"
- [ ] `TestArchitectureEpochNotesExcludeLLM`(同文件,新):architecture 对子说只由 epoch 覆盖的是"不是 `LLM-` ID 的 note",
  不说 "permission and note checks" / "权限和 note"
- [ ] `TestEpochTriggerIsStatedAlike`(同文件,新):architecture 对子、spec §5.1 的 epoch 纪律、`rulesEpoch` 注释都把 advisory 算进
  触发条件,并点名 `collect` 的凭据 import 检查(`EXFIL-005`)
- [ ] `TestEpochRuleLoadsWhereCoveredCodeIsEdited`(`cmd/aguard/claude_rules_test.go`,新):`.claude/rules/pipeline.md` 在七个表外检测文件
  (shape / hooks / logical / imports / permcheck / gate status / main)被编辑时都会加载 —— 没有开头的 frontmatter 就是常驻(`frontmatterPaths`
  的读法),有就要求 `paths:` 覆盖这七个 —— 且有一行同时点名 `detect.rulesEpoch`、`builtinRules()`、`make docs`。
  **变异**:frontmatter 生效时把 `paths` 收窄到 `internal/detect/**` → 红
- [ ] `TestE2E_ReportNamesItsRules`(`cmd/aguard/e2e_test.go`,新):`scanEnv` 和 `checkTarget` 的结果 `RulesVersion == detect.RulesVersion()`,
  `--json` 序列化里有 `"rules_version"` 键。今天 `ScanResult` 没有这个字段,编译即红
- [ ] `TestBinaryVersionLine_RulesComeAfterTheExistingFields`(`cmd/aguard/main_test.go`,新):`binaryVersionLine(...)` 的 `strings.Fields()[1]` 是版本号,
  前缀与今天的格式逐字相同,行末是 ` · rules=<v>`。**反向**:`$2` 契约(`release.yml:60`)不变
- [ ] `TestRulesDocHeaderCarriesRulesVersion`(`hack/gen-rules/main_test.go`,新):提交的 `docs/rules.md` 含当前 `RulesVersion()`
- [ ] **反向断言**:`TestEveryRuleIDIsDocumented`、`TestDocumentedMetadataMatchesEngine` 不改一字仍绿 —— 规则集本身一条没动
- [ ] **反向断言**:`TestHashGolden`(`internal/collect/hash_test.go`)不改一字仍绿 —— `rules_version` 是另一个哈希,artifact 的 canonical 哈希、
  信誉库和闸门批准的 key 一个都不动
- [ ] **反向断言**:`TestPluginVersionLine` 与 `cmd/aguard/buildinfo_test.go` 不改一字仍绿 —— 插件那一行和 build info 回填都不动
- [ ] `make verify` 绿;真机 `scan` 的 summary 行与改动前相同(本条不改检测)

## 不做什么

- **不做 `inputs_digest`,不做 `aguard verify`**:输入摘要与离线复核是另外两件事,本条只交付 `rules_version`
- **人类可读报告不加任何东西**:text / markdown / html / sarif 一个字节不变(`internal/report` 零改动)。markdown 页脚已有 `AgentGuard <version>`,
  要不要加规则版本是另一件事
- **不动 artifact canonical 哈希、信誉库、闸门**:`internal/collect`、`internal/reputation`、`internal/gate` 零改动;批准记录仍只记 `tool_version`
- **不动 `clean --json`**:`CleanPlan` 不加字段,`CleanPlanSchema` 不变 —— 清理项不是规则产物
- **不动 baselines**:`baselines/` 零改动,已提交的 `results/` 一个字节不变。adapter 读 `aguard version` 首行,今后的 `run.yaml` 自然带上 `rules=`
- **不动 `plugin/`**:`references/install.md` 那句"prints version, commit, build date, and the reputation-list entry count"仍然为真,不为一个后缀发插件
- **不动 `buildinfo.go` 和已有的 `versionLine`**:version / commit / date 的来源不变,插件那一行不变
- **不改任何规则**:`builtinRules()` 一条不动,`docs/rules.md` 只有页眉多一段
- **不动 `.claude/rules/detect.md`**(198/200 行);`.claude/rules/` 里只有 `pipeline.md` 多一行
- 不加依赖,`go.mod` / `go.sum` 不动

## 不能说什么

- **不说"`rules_version` 相同 ⇒ 检测逻辑相同"**。规则表那一半是机械的;表外的检测代码只靠手动 `rulesEpoch` 纪律,忘了加一就是两份
  "同一版规则"的报告其实出自不同的检测代码。`docs/rules.md` 页眉只说可以机械保证的两句:不同 ⇒ 规则变了;相同 ⇒ 引擎规则表相同
- **不说 `rules_version` 覆盖判官,不说"`rules_version` 相同 ⇒ 判官相同"**:`internal/judge` 的提示词、证据落地、共识、严重度钳制
  一样都不在里面,改它们不加 epoch
- **不说报告里有别的东西标识了判官**:`JudgeSummary` 只有 ran / reason / artifacts / calls / failed / skipped / findings / endpoint,
  没有模型、提示词版本、`samples`、`authority`;`llm` 配置根本不进报告。今天报告里标识判官代码的**只有 `tool_version`**,能说的就这一句
- **不说"`rules_version` 哈希了决定一条发现的东西",不说它覆盖 `docs/rules.md` 上的每一条**:哈希只进 `builtinRules()` 的 46 条;
  13 条结构化、6 条权限、6 条不是 `LLM-` ID 的 scan note、`GATE-001` 只靠 epoch 纪律;**每个 `LLM-` ID**(7 条判官发现 + 3 条 `LLM-` note
  `LLM-000/002/005`)完全不在里面。一句 "It hashes what decides a finding — each rule's ID, …" 放在一页 83 个 ID 的顶上,读起来就是全覆盖;
  "scan note、闸门条目只靠 epoch" 和 "`LLM-` 条目(含 note)在外面" 两句并列,`LLM-` note 同时落在两边,读起来自相矛盾
- **不说 `GATE-000` 被 epoch 覆盖**:它是闸门给 hook 的回复,不进任何报告,页眉对它不作任何声明
- **不说 epoch 覆盖了 `REP-BAD` / `REP-GOOD` 的结果**:页眉把它们分别算进"13 条结构化"和"6 条 scan note",epoch 管的是产出它们的**代码**;
  它们响不响取决于信誉名单,而名单不在 `rules_version` 里(未决问题 3)
- **不说"`rules_version` 相同、输入相同 ⇒ 报告字节相同"**:`Title`/`Why` 文本、`tool_version`、`scanned_at`、评分权重、信誉名单都不在里面
- **不说"报告现在能自证 / 可复算验证"**:`inputs_digest` 和 `verify` 没做
- 不说 Title/Why 改动"不影响报告":它们照样出现在 JSON 的 finding 里,只是不进版本号

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 新测试,跑红 | `detect, cmd, gen-rules: tests — no report, version line or rules.md header says which rule table produced it (P-002)` |
| 2 | `detect.RulesVersion` + `rulesVersion` + `rulesEpoch` | `detect: RulesVersion hashes what decides a finding, plus an epoch for detection code outside the table (P-002)` |
| 3 | `ScanResult.RulesVersion` 在 `analyze` 里填;`binaryVersionLine` 抽出并追加 `rules=` | `model, cmd: scan/check --json and aguard version name the rule table that produced them (P-002)` |
| 4 | `gen-rules` 页眉;`make docs` | `gen-rules: the docs/rules.md header carries the rules version, so the drift check now catches a pattern change too (P-002)` |
| 5 | spec §5.1 / §8;README 与 zh 对子;architecture 与 zh 对子 | `docs: spec, README and architecture pairs say what rules_version covers and when the epoch moves (P-002)` |
| 6 | 判官移出 `rules_version` 的范围(人裁定)—— `rulesEpoch` / `RulesVersion` 注释、spec §5.1 / §8、architecture 对子、`model.go` 注释、rules.md 页眉;两条测试 | `detect, gen-rules, docs: rules_version covers deterministic detection only, so a judge change no longer obliges an epoch bump nobody would see (P-002)` |
| 7 | rules.md 页眉说清哈希的是 N 条引擎规则,其余条目只靠 epoch;architecture 对子同一句一起收窄;一条测试 | `gen-rules, docs: the rules.md header names the engine rules it hashes and says the rest of the page rides on the epoch alone (P-002)` |
| 8 | `.claude/rules/pipeline.md` 加一行 epoch 纪律;一条测试 | `rules, cmd: the epoch rule loads wherever deterministic detection outside the rule table is edited, not only beside the constant (P-002)` |
| 9 | `TestRulesVersion_EveryRuleFieldIsDecided` 逐字段变异 | `detect: a Rule field listed as hashed must move the rules version when changed, so a list entry can no longer stand in for the hash (P-002)` |
| 10 | 报告里标识判官代码的只有 `tool_version` —— spec §5.1 / §8、rules.md 页眉(经 gen-rules)、architecture 对子、`rulesEpoch` 与 `ScanResult.RulesVersion` 注释不许诺 `judge` 摘要和 `llm` 配置说明判官;一条新测试、两条收紧 | `detect, gen-rules, docs: a report identifies the judge's code only through tool_version, and no passage promises the judge summary does more (P-002)` |
| 11 | rules.md 页眉每类 ID 只说一次(引擎规则进哈希;结构化、权限、非 `LLM-` note、`GATE-001` 靠 epoch;每个 `LLM-` ID 在外),不提 `GATE-000`;`make docs`;一条测试重写 | `gen-rules: the rules.md header states each ID class once — engine rules hashed, the other deterministic IDs on the epoch, every LLM- ID outside — so its sentences no longer contradict (P-002)` |
| 12 | architecture 对子里只由 epoch 覆盖的 note 写成"不是 `LLM-` ID 的 note";一条测试 | `docs: the architecture pair puts only the notes that are not LLM- IDs on the epoch, so it no longer contradicts its own next sentence that puts the judge outside (P-002)` |
| 13 | architecture 对子的 epoch 触发条件加上 advisory,表外代码点名 `collect` 的 `EXFIL-005` 检查;一条测试,同时钉住 spec §5.1 和 `rulesEpoch` 注释 | `docs: the architecture pair's epoch trigger reads like spec §5.1 — the advisory flag counts and collect's EXFIL-005 check is named — so neither change looks exempt from a bump (P-002)` |
| 14 | 本文件、索引 | `proposals: P-002 (P-002)` |

W1–W13 在旧仓按同样的顺序做过并经过两轮 review;本仓逐个重放,冲突与本仓的差异在对应提交里解决,证据在本仓重测:
`versionLine` 已被插件那一行占用 → W3 用 `binaryVersionLine`,并在提交说明里写明 version / commit / date 照旧来自 `-ldflags` 或
`applyBuildInfo`;`EXEC-012` 让引擎规则变成 46 条 → W4 起 `docs/rules.md` 由本仓 `make docs` 重新生成;本仓带 `paths:` 的
rules 文件第一行是 SPDX 注释 → W8 的测试按 `frontmatterPaths` 的读法把"没有开头的 frontmatter"算作常驻(见「完成」)。

## 未决问题

1. **要不要像 `TestHashGolden` 那样给版本号钉一个字面量?**
   **建议**:不钉。这个值**就应该**随每次规则改动而变,钉了字面量,每个改规则的提交都得顺手改它 —— 一条唯一正确的应对是"改数字"的测试
   什么也不保护(`e2e_test.go` 头部拒绝钉分数字面量也是这个理由)。`TestHashGolden` 不一样:信誉库条目和闸门批准**以它为 key 存着**,
   定义一动全部失效;没有任何东西以 `rules_version` 为 key 存储。发布出去的值在 `docs/rules.md` 页眉,CI 的漂移检查就是那条
   "代码和公布值不一致就红"的断言;`TestRulesDocHeaderCarriesRulesVersion` 在 `go test` 里再钉一次。
   **已决(2026-10-08)**:按建议。
2. **按引擎顺序哈希,还是先按 ID 排序?**
   **建议**:引擎顺序。同一行上的发现按规则顺序 append(`detect.go:321`),`byID` 取第一个匹配,所以只交换两条规则的顺序也可能改变报告;
   排序会把这种变化藏起来。代价是纯粹的重排也会让版本号变 —— 那是往"规则变了"那一侧错,安全侧。
   **已决(2026-10-08)**:按建议。
3. **评分权重、信誉名单进不进 `rules_version`?**
   **建议**:不进。"两份报告为什么不同"的原因分三种 —— 规则变了 / 输入变了 / 分数不可复现,评分属于第三种,混进来就分不开了;
   信誉名单在 version 行上已经有自己的计数(`reputation entries=N`),而且 `--no-reputation` 能关掉它。
   **已决(2026-10-08)**:按建议。
4. **`rulesEpoch` 要不要机械强制(比如哈希 `internal/detect` 的源码)?**
   **建议**:不。任何源码哈希都会被只改注释的提交推动,一个改注释就变的版本号等于没有版本号 ——
   本条自己的 W6、W10、W12、W13 就只改注释、文档和测试,`rules_version` 一个字符都不该动。代价写进「不能说什么」第一条。
   **已决(2026-10-08)**:按建议。
5. **字段总是出现,还是 `omitempty`?**
   **建议**:总是出现。一次扫描总有规则表,空值没有合法含义;消费方读到"没有这个键"就知道是本条之前的报告。
   **已决(2026-10-08)**:按建议。
6. **判官进不进 `rules_version` / epoch 的范围?**(旧仓 review 时提出)
   **建议**:不进。`rules_version` 的用途是离线复算 `overall`,判官只动 `overall_effective`;把判官的规则映射列进 epoch,
   改 `internal/judge` 的人看不见这份义务。
   **已决(2026-10-08,人)**:按建议,判官移出范围(W6)。

**真机扫描头部(实现阶段记录,不是问题)**:`scan --root ~/.claude --quiet --json`,origin/main dec64ca 构建 vs 本分支(W13 之后),只记汇总行:

```
before: overall 69 · overall_effective 69 · artifacts 175 · scoring findings high 162 / medium 451 / low 193 · notes 10 · rules_version (absent)
after:  overall 69 · overall_effective 69 · artifacts 175 · scoring findings high 162 / medium 451 / low 193 · notes 10 · rules_version 43f245966105
JSON 除 scanned_at / tool_version / rules_version 三个键外逐键相同;逐 (rule_id × severity) 的发现计数相同
```

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-002 找)
发布:待发
证据:W1 在本仓 origin/main dec64ca 上编译红,原因与判据一致:internal/detect 无 RulesVersion / rulesVersion / rulesEpoch;cmd/aguard 无 detect.RulesVersion、model.ScanResult 无 RulesVersion 字段、无 binaryVersionLine;hack/gen-rules 无 detect.RulesVersion
证据:TestRulesVersion_MovesWithWhatDecidesAFinding(internal/detect/rules_version_test.go);W2 后绿:14 种变异(正则、加 except、去 except、严重度、维度、ID、Advisory、四个 *Only、交换两条顺序、删一条、epoch + 1)各自让版本号变且两两不同;手工负向:临时从哈希里删掉 except 字段 → "except added / removed" 两条红,已还原
证据:TestRulesVersion_IsStable(同文件);W2 后绿:两次调用相等、12 位小写十六进制、等于 rulesVersion(builtinRules(), rulesEpoch);本仓当前值 43f245966105(46 条引擎规则,含 EXEC-012)
证据:反向断言 TestRulesVersion_IgnoresProse(同文件):46 条规则的 Title / Why / Ref 全部改写 → 版本号不变
证据:TestRulesVersion_EveryRuleFieldIsDecided(同文件,W9):Rule 的 13 个字段 = 10 个进哈希 + 3 个只是文字;每个字段在 INJ-001 的副本上改一次,进哈希的都让版本号变、文字的都不变。W9 的红是变异出来的:① 把 Title 从文字清单挪进哈希清单 —— W9 前的测试 PASS,W9 的测试 FAIL "Rule.Title is listed as hashed, but changing it does not move the rules version";② 给 Rule 加 DocOnly bool 并只登记进哈希清单 —— FAIL "Rule.DocOnly is listed as hashed, but changing it does not move the rules version";两次变异都已还原
证据:TestRulesEpoch_ScopeIsDeterministicOnly(internal/detect/rules_version_test.go)与 TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion(hack/gen-rules/main_test.go),W6:放到 W5 的树上红 5 + 2 条(缺 "DETERMINISTIC detection only"、缺 "The LLM judge is outside the rules version"、列着 clampSeverity / LLM-007 / "the judge's rule mapping";页眉缺 "It covers deterministic detection only." 与 LLM 条目在外那句)→ W6 后绿
证据:TestRulesDocHeaderSaysWhatTheHashCovers(hack/gen-rules/main_test.go),W7:放到 W6 的树上红 3 条(缺 "It hashes the 46 engine rules' ID, dimension, severity, flags and pattern"、缺 "The structural, permission, scan-note and gate entries on this page are covered only by that epoch"、仍有 "what decides a finding")→ W7 后绿;46 取自 len(detect.Rules())
证据:TestEpochRuleLoadsWhereCoveredCodeIsEdited(cmd/aguard/claude_rules_test.go),W8:旧仓原样的测试放到 W7 的树上红 8 条 —— 缺那一行 1 条,外加 7 条 "does not load … (paths: [])",而且在 W8 的树上那 7 条照样红。它们不是 paths 太窄:本仓每个带 paths 的 rules 文件第一行是 SPDX 注释,frontmatterPaths 读不到开头的 --- 块,按"无 frontmatter = 常驻"处理(TestClaudeRulesAreScopedToExistingPaths 也是这样读的)。测试按同一读法改:无 frontmatter 算加载,有就必须覆盖七个文件;改后的测试放到 W7 的树上只红 1 条(缺那一行)→ W8 后绿。变异:删掉 pipeline.md 那一行 → 红 1 条;去掉 SPDX 行让 frontmatter 生效、paths 不变 → 绿;再把 internal/** 收窄成 internal/detect/** → 红 3 条(imports.go、permcheck.go、gate/status.go);都已还原。pipeline.md 151 → 152 行(上限 200)
证据:TestRulesVersionDocs_IdentifyTheJudgeOnlyByToolVersion(internal/detect/rules_version_docs_test.go)与收紧后的 TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion,W10:放到 W9 的树上红 12 + 2 条(rulesEpoch 注释、ScanResult.RulesVersion 注释、architecture 对子、spec §5.1、spec §8 六段各缺 "only through tool_version" / "只通过 tool_version"、各仍有 "how it ran" / "怎么跑的" / "Judge 说明";页眉缺 "today a report identifies the judge's code only through `tool_version`"、仍说判官怎么跑的)→ W10 后绿
证据:TestRulesDocHeaderSaysWhatTheHashCovers(W11 重写):放到 W10 的树上红 5 条,同时 TestRulesDocHeaderPutsTheJudgeOutsideRulesVersion 红 1 条 → gen-rules 改页眉 + make docs 后绿;分类断言靠变异证明:① gateNotes 加一条 GATE-002 → 红 "GATE-002 is in 0 header classes";② 把 LLM-010 放进 structural → 红 "LLM-010 is in 2 header classes [epoch outside]"(计数句同时红);两次都已还原
证据:TestArchitectureEpochNotesExcludeLLM(internal/detect/rules_version_docs_test.go),W12:放到 W11 的树上红 4 条(对子两边各缺 "the notes that are not LLM- IDs" / "不是 LLM- ID 的 note",各仍有 "permission and note checks" / "权限和 note")→ W12 后绿
证据:TestEpochTriggerIsStatedAlike(同文件),W13:放到 W12 的树上红 4 条,全在 architecture 对子(两边各缺 advisory 那半和 EXFIL-005 那句)→ W13 后绿;变异:从 rulesEpoch 注释删掉 "or advisory flag" → 红 1 条;从 spec §5.1 的 epoch 纪律删掉 "/advisory" → 红 1 条;都已还原
证据:rules_version 从 W4 到 W13 一直是 43f245966105 —— W6–W13 只改注释、文档、生成器页眉文字和测试,rulesEpoch 仍是 1;每个改了生成器的提交之后 make docs 都已提交、无漂移
证据:TestE2E_ReportNamesItsRules(cmd/aguard/e2e_test.go);W1 编译红 → W3 后绿:scan 与 check 两个入口 rules_version == detect.RulesVersion(),--json 有 rules_version 键,tool_version 仍在
证据:TestBinaryVersionLine_RulesComeAfterTheExistingFields(cmd/aguard/main_test.go);W1 编译红 → W3 后绿。真机:make build 的 aguard version 首行 "aguard v0.18.0-15-gc2d0a4f (commit c2d0a4f, built …) · reputation entries=18 · rules=43f245966105",awk '{print $2}' = v0.18.0-15-gc2d0a4f(release.yml:60 的契约不变);不带 ldflags 的 go build 首行 "aguard dev (commit none, built unknown) · reputation entries=18 · rules=43f245966105" —— dev 构建也说得出规则表
证据:TestRulesDocHeaderCarriesRulesVersion(hack/gen-rules/main_test.go);W2 之后在 origin/main 的 rules.md 上红("does not name rules version 43f245966105")→ W4 make docs 后绿
证据:反向断言不改一字仍绿 —— TestEveryRuleIDIsDocumented、TestDocumentedMetadataMatchesEngine(hack/gen-rules/main_test.go)、TestHashGolden(internal/collect/hash_test.go)、TestPluginVersionLine、TestPluginVersionLine_LegacyName、TestApplyBuildInfo(cmd/aguard)
证据:真机 scan,origin/main 构建 → 本分支:overall 69 → 69、overall_effective 69 → 69、artifacts 175 → 175、计分发现 high 162 / medium 451 / low 193 两边相同、notes 10 → 10;JSON 除 scanned_at / tool_version / rules_version 外逐键相同;逐 (rule_id × severity) 计数相同
证据:不做什么 —— git diff --stat origin/main -- internal/report internal/collect internal/gate internal/reputation internal/judge internal/permcheck internal/clean baselines plugin .claude/rules go.mod go.sum internal/detect/rules_data.go internal/detect/rules.go cmd/aguard/buildinfo.go cmd/aguard/buildinfo_test.go 只剩 .claude/rules/pipeline.md 一行(detect.md 未动);docs/rules.md 只多页眉 17 行;cmd/aguard/version.go 只多 binaryVersionLine 一个函数,已有的 versionLine 一字未动
证据:make verify: all gates passed;go version go1.23.5(无工具链切换);go.mod 第二行 go 1.23.5,module github.com/basdotio/AgentGuard,无新依赖
```
