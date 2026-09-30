<!-- SPDX-License-Identifier: MIT -->
# 022 — 最像我们的对照物,却在语料里批着自己出的卷子

- **来源**:新发现(2026-09-24,对话提出);`competitor-research-2026-09` §3.4、`audit-log` 条目 4.D
- **依赖**:P-017(**已合入 dev**,2026-09-24)—— `pick()`、共享的 `baselines/adapter/sarif`、`Placement()` 接口都已在主干上,本条直接接
- **分支**:`p/022-cisco-skill-scanner`

## 问题

P-017 之后量测台有两列数,而 cc-audit 和我们**不在同一条战线上**:它把阈值放得比我们低得多,
抓到 2.2 倍的恶意样本、也标红 6–12 倍的良性样本。一笔召回/精度对调说明它是另一种取舍,
说明不了我们的取舍**在同类里**是什么水平。

**Cisco skill-scanner 是调研过的所有工具里最像我们的一个**——`competitor-research` §3.4
写的是"默认最克制":

- 默认路径**零 LLM、零联网、零账号**,所有联网分析器默认关闭(和我们的不变量 #1 同形)
- **默认确定性**;四种 CI 门禁形态(`--fail-on-severity` / `--fail-on-findings` / pre-commit / Action)
- 唯一一家公开了**可复现**的 P/R,并且**自判不及格**
- 三个规则包:core 189 + ATR 712(第三方标准,单向溯源)+ promptguard 26
- 目标客户是 **AppSec 团队 / CI owner**,和我们一样

同一份文档把它排在"这家最值得学"。可到今天,我们和它之间**没有一个数**。

**而它在语料里有一个特殊位置,让"跑一遍"不能直接等于"比一比":** 语料里 300 个恶意样本中
**127 个派生自 `cisco-mcp-scanner-evals`** —— 它自己的评测集。让它跑这 127 个,是它在批自己
出的卷子。剔掉之后(2026-09-24 用 `corpus samples --exclude-source cisco-mcp-scanner-evals` 实测):

| | 全量 | 剔除 cisco 来源 |
|---|---|---|
| 恶意合计 | 300 | **173** |
| 恶意 · mcp 面 | 130 | **3** |
| 恶意 · skills 面 | 153 | 153 |
| 样本总数 | 3,539 | 3,412 |

所以**它在 MCP 面上不可能被公平测量**(干净分母 n=3),能比的是 skills 面(n=153,够出一个
Wilson 区间)和另外四个小面。P-015 加 `--exclude-source` 就是为了这一刻,但那个开关到现在
一次都没被真用过。

**还有一条不是数字的事。** `bytecode_analyzer.py` 对被扫的 `.pyc` 调 `marshal.load()`,
CPython 文档明说这个原语 "is not intended to be secure against erroneous or maliciously
constructed data"。语料里**恰好有一个 `.pyc`**——在 `malicious/skills/sg-bytecode-poisoning/`,
一个专门讲字节码投毒的恶意样本。也就是说这次全量运行会把一个为投毒而造的 `.pyc` 喂进一个
文档写明不防投毒的反序列化调用。这不是"执行被扫内容",但离得不远;`tools.yaml` 的
`executes_scanned_content` 该怎么填,得先看清这一条。

## 初步方向

第三个 adapter,`baselines/adapter/cisco/`,接在 P-017 建好的口子上——`pick()` 加一个 case,
SARIF 用共享的 `sarif.Fold`,`Placement()` 说清放置。中立那半一行不动。

按 tag `2.1.0` 读 README(未执行):

| | 事实 | 意味着 |
|---|---|---|
| 入口 | `skill-scanner scan <dir>`,`--format sarif\|json`,`--fail-on-severity` | 和 cc-audit 同形,吃目录 |
| 分发 | PyPI `cisco-ai-skill-scanner` 2.1.0,Apache-2.0;GitHub release 无二进制资产 | 钉 pip 版本 + PyPI 的 sha256,不是 tarball |
| 默认档 | `--use-behavioral` / `--use-llm` / `--use-aidefense` 全是 opt-in | 默认档 = 纯静态,正是我们要比的那档 |
| 非 skill 制品 | `--lenient`,`--skill-file <name>` | **放置问题在这里**:`scan` 预期一个带 `SKILL.md` 的目录;`.mcp.json` / `settings.json` / hooks 这些面它怎么吃,要实测 |

**两个分母都出,都提交**:全量一份,`--exclude-source cisco-mcp-scanner-evals` 一份。对照表里
Cisco 那列**只引用后者**,并在 MCP 面上明写"不可测(干净 n=3)"而不是留空。

**先探针再全量**(P-017 的教训,三处猜测全错):一个恶意 skill、一个良性 skill、一个 `.mcp.json`
样本、一个空目录,再单独跑 `sg-bytecode-poisoning` 看 `marshal.load()` 那条路——
这五个跑完再决定 `executes_scanned_content` 填什么、非 skill 面要不要 `--lenient`。

P-017 已合入,本分支从 `origin/dev` 开出来时口子就在脚下——`pick()` 加一个 case、
`sarif.Fold` 直接调、`Placement()` 照接口填。不再有上一次那种"复制一遍还是等"的选择题。

## 实测(2026-09-24,W2 探针;全量见下一节)

装法:`uv venv --python 3.12 /tmp/cisco-venv`;先 `pip download --no-deps` 拿主 wheel,
sha256 **与 PyPI 声明一致**(`452811a6…852bf6`),再从本地 wheel 装,依赖走 PyPI。98 个包。
`skill-scanner --version` → `skill-scanner 2.1.0`。

### 五个探针 + 一个目录不存在

| 探针 | exit | stdout | 结论 |
|---|---|---|---|
| 恶意 skill(`authorized-keys-persist`,payload 全在 SKILL.md) | 0 | SARIF,1 条 `note` | **什么都没抓到**,只报 `MANIFEST_MISSING_LICENSE`——和良性一样 |
| 良性 skill | 0 | SARIF,1 条 `note` | 同上 |
| `.mcp.json` 样本(无 SKILL.md) | **1** | 空 | stderr `Error loading skill: SKILL.md not found in …` |
| 空目录 | **1** | 空 | 同上——**它对空目录和对 MCP 样本说的是同一句话** |
| `sg-bytecode-poisoning`(那个 `.pyc`) | 0 | SARIF,2 条,一条 `error` | `BYTECODE_ANALYSIS_UNAVAILABLE`,JSON 里 HIGH/obfuscation。见下 |
| `/nonexistent` | **1** | 空 | stderr `Error: Directory does not exist` |

三处设计假设被证实、一处被推翻、一处新发现:

- **证实**:SARIF run 的键是 `invocations / results / tool`,**没有 `artifacts`** → `ArtifactsNotEmitted`。
  `kind` 缺席。stdout 干净(首字节 `{`),没有 cc-audit 那种日志行污染。
- **证实**:exit 1 有两个意思。上表三种 exit 1 里没有一个是闸门。adapter 只看它印了什么。
- **证实**:无 SKILL.md → `Error loading skill`,进 `no-verdict / unsupported-input`。
- **推翻**(部分):"默认档会抓到 skill 面上的恶意样本"。第一个探针一无所获。但 20 个恶意 skill 的
  小样本里 8 个 `max_severity` ≥ HIGH(4 CRITICAL、4 HIGH),规则以 `PIPELINE_TAINT_FLOW`、
  `CORRELATED_OBFUSCATION_EXECUTION_FLOW`、`YARA_*` 为主——**不是系统性不报**,是那一个样本漏了。
  全量决定。
- **新发现**:`MANIFEST_MISSING_LICENSE`(INFO)在 20 个 skill 里报了 19 个,并把 `is_safe` 翻成
  false。所以 `is_safe` 不能当"干净"读;fixture 判据里"说了"要以 INFO 以上为准。

### `marshal.load()` 那条路

本 venv 是 Python 3.12,语料那个 `.pyc` 是 `cpython-314`。magic 不匹配,`_load_pyc_code` 在
`marshal.load()` 之前就 raise,工具报:

> Bytecode file scripts/utils.cpython-314.pyc has matching source scripts/utils.py, but the scanner
> could not compare them: bytecode targets a different Python version … **Unverifiable bytecode can
> conceal code that is absent from visible source.**

这是一条**披露**(它没读懂的东西它说了),HIGH 级,category `obfuscation`。**但在 3.14 运行时上
`marshal.load()` 会被走到。** `executes_scanned_content: false` 成立于"反序列化不是执行",
`tools.yaml` 的注释把这两句都写了。

### policy

`generate-policy -o` 导出 `balanced`(527 行,sha256 `22609f6a…`)。传 `--policy <导出件>` 与
不传:去掉 `invocations`(只有时间戳)后 SARIF **逐字节相同**。哈希进了 `run.yaml`。

### 阶梯与退出码(实测)

`--fail-on-severity` 对那条 HIGH finding:`critical` → 0,`high/medium/low/info` → 1。
证实 `threshold: high` = 它的默认门禁,且 SARIF `error` ↔ HIGH+。

JSON 顶层:`is_safe`、`max_severity`(INFO/LOW/MEDIUM/HIGH/CRITICAL)、`findings[].{rule_id,severity,category,analyzer}`。
**没有 0–100 分**(README 里那个分是"看懂了多少",且不在 JSON 输出里)。所以第二趟取严重度和
category,不取分数。

默认 analyzer 三个:`static`、`bytecode`、`pipeline`;`behavioral` / `virustotal` / `aidefense` opt-in。

### 规则 id 的形状(W7 的 `rule_id_pattern` 要用)

`MANIFEST_MISSING_LICENSE` · `PIPELINE_TAINT_FLOW` · `BYTECODE_ANALYSIS_UNAVAILABLE` ·
`CORRELATED_OBFUSCATION_EXECUTION_FLOW` · `YARA_jailbreak_generic` ·
`YARA_SUSP_Lnx_EncodedShell_DecodeExec_Sep26` —— 大写下划线,YARA 前缀的带小写与日期。
**不是** `[A-Z]{2,3}-\d{3}`。

### Smoke(driver 端到端,10 个混合样本,`-j 6`)

`1 scored · 9 no-verdict · 0 error`,12 s → 约 1.2 s/样本。全量 3,539 约 70 分钟,后台跑。

## 实测(2026-09-24 → 25,全量 #1:3,539,默认档,策略 balanced,`-j 6`,1h29m)

`ledger: 2642 scored · 897 no-verdict · 0 error = 3539 of 3539`。**判据 1 成立**:三态划分完整,
897 个 no-verdict 全部是 `unsupported-input` 并带 loader 原话。

### 两次被 tripwire 拒绝,两次都对

1. 第一遍:**mcp 面 0 flagged of 130 → `needs-declaration`**,拒绝发布。原因是一个事实:strict 档要
   `SKILL.md`,487 棵 mcp 树一棵都没有。从 raw 数出 mcp 0/487、connector 1/126、hooks 1/196、
   permission 2/178、skills 2636/2636 → 写声明。
2. 第二遍:**permission 面 `stale-declaration`,1 flagged of 6**。我把 connector/hooks/permission 也
   声明成"不读",而 permission 有 2 棵带 SKILL.md、1 棵被抓到。声明的语义是"什么都不读",
   部分覆盖是账本里的事实,不是声明。**只留 mcp。** 第三遍发布。

这是 P-015 建的那道闸门第一次在真实数据上触发,两个方向(漏声明、声明过头)都拦住了。

### 「批自己的卷子」这个问题在默认档下不存在

`cisco-mcp-scanner-evals` 127/127 **全部 no-verdict**——它们是 `.mcp.json`,strict 档不认。
记分卡的"未评来源"表把它列成"整个来源未评"。它没有批自己的卷子,**它连卷子都没打开**。
第二个分母(剔除该来源)因此在 mcp 上和第一个分母一样是空的,差别只在总数 3,539 vs 3,412。

### 数(全量分母;头行以 excl-cisco 那份为准,见下节)

| | skill-scanner |
|---|---|
| 恶意 flagged | **27/300**(在它 scored 的 143 个 read 基里 25 个) |
| 良性 flagged | 76/3220 |
| 硬否定 | 1/4(只有 4 个被评,其余 15 个是 mcp/permission 面,被拒) |
| skillsgoat(65) | 11% [5, 21] |
| skillcraft-audit(42) | 10% [4, 22] |
| datadog-ai-skills(22) | 10/22 |
| skillet-wild(474,良性) | 6% [4, 8] |
| skillmd-138k(1996,良性) | 2% [2, 3] |
| attribution | 11/27 = 41% [25, 59] —— **按名字的映射,证据等级低于 P-017** |

规则 tally(malicious):`PIPELINE_TAINT_FLOW` 8 · `YARA_SUSP_Lnx_EncodedShell_DecodeExec_Sep26` 7 ·
`CORRELATED_OBFUSCATION_EXECUTION_FLOW` 6 · `YARA_command_injection_generic` 4。
良性池上最多的是 `CORRELATED_NETWORK_EXECUTION_FLOW` 15、`PIPELINE_TAINT_FLOW` 14。

### 反向断言(2026-09-25)

`origin/dev` 开 worktree 构建 aguard,与本分支各跑一次同一份 3,539 工作表:两边都是
`3539 scored · 0 no-verdict · 0 error`、恶意 87/300,`verdicts.jsonl` **`cmp` 逐字节相同**。
本分支没有动 aguard 的任何一个答案。**判据 2 成立。**

### 全量 #2(excl-cisco,3,412,1h21m):第二个分母是多余的一次运行

`ledger: 2642 scored · 770 no-verdict · 0 error = 3412 of 3412`。与 #1 在 3,412 个共同样本上
**判决逐行一致**(outcome / verdict / severity / reason / dimensions 零差异);只在 #1 的 127 个
全部是 no-verdict。恶意 flagged 27/173。

**但记分卡是同一份。** `corpus score` 对全部 3,539 个标签打分、不认工作表,开头仍是
`2642 of 3539 scored; 897 had no verdict`,`cisco-mcp-scanner-evals 127 of 127 (the entire source)`
照样列在"未评来源"里。它的"未评不算过、召回只算 scored 子集"规则**已经**把那 127 个排除在
分母外了——第二次运行没有改变任何一个数,只改了账本行数和 `run.yaml` 的 `work_list_size`。

已决 2 的前提("两个分母会给出两个不同的数")在 strict 档下不成立:被剔除的来源恰好是它
整个读不了的那个面。**结果目录保留两份**——它们是两次真实运行,`run.yaml` 各自说明分母——
但对照表只需引用一份,MCP 行写"不可测(strict 拒绝 487/487)",不写 n=3。

### 六个 fixture

| fixture | 结果 | |
|---|---|---|
| symlink-cycle | pass | exit 0,1 finding(INFO) |
| symlink-escape | pass | exit 0,2 findings,MEDIUM |
| **deeply-nested** | **fail** | **崩了**:`OSError: [Errno 63] File name too long` 未捕获,7 层 traceback 死在 `pathlib.stat`。语料原话:"A crash here is a crash on any pathologically nested archive" |
| traverse-only-dir | fail | INFO 以上零、没提那个读不了的目录,`is_safe=true`——对一个一半读不了、里面是 `curl \| sh` 的 skill 开了干净证明 |
| sparse-huge-config | untestable | 那个 fixture 没有 SKILL.md(是 `.mcp.json`),strict 拒绝,walker 没跑 |
| fifo-as-skill | ~~untestable~~ → **pass**(重跑后) | 它报 `Path is not a regular file` 拒绝——正是语料要的"stat 一下不是普通文件就跳过"。**第一版判据把它归成 untestable 是我的错**,已修(`fcdd836`),结果目录要重跑一遍才会体现 |

### 时间

3,539 个样本 1h29m,约 6 s/样本(两趟 Python 启动 + 900 条规则 + YARA)。cc-audit 是 3m22s。

## 完成的判据

- [x] **每个样本都有一行,且没有 SKILL.md 的样本不是 error 也不是 benign。**
      `ledger.jsonl` 3,539 行,`ledger.Check` 零 error;`class != skills` 的样本在默认(strict)档下
      记为 `no-verdict / unsupported-input` 并带 loader 的原话 —— 那是「它拒绝这个输入形状」,
      不是「它没找到」。**这一条会第一次让 `surfaces_declared_uncovered` 拿到有证据的声明。**
- [x] **反向断言(P-017 定下的那种形式)**:在 `origin/dev` 开 worktree 构建 aguard,跑同一份
      工作表,与本分支上同一次运行的 `verdicts.jsonl` `cmp` 逐字节相同。
- [x] **`TestPickDispatchesToCisco`**:`pick()` 给 `-tool skill-scanner` 返回配好的 `*cisco.Adapter`,
      `Bin` / `Threshold` / `Policy` 三样都来自 flag,不来自没人选过的默认值。
- [x] **`TestAnExitOfOneIsNotAlwaysTheGate`**:Cisco 的运行失败(目录不存在、taxonomy 加载失败、
      `SkillLoadError`)**也返回 1** —— 与闸门响同码。stdout 不是 SARIF + stderr 以 `Error` 开头
      → `Errored` 或 `NoVerdict`,**永不** `Scored`。这是 Heeler「No credentials found 也 exit 1」
      那个坑的同一形状,`sarif.IsRunFailure` 的 0/1 契约对它不成立。
- [x] **`TestNoSkillMdIsUnsupportedInputNotAMiss`**:strict 档下没有 `SKILL.md` → `NoVerdict` +
      `UnsupportedInput`,detail 带 loader 的 `Error loading skill:` 原话。
- [x] **两个分母都提交**(两份都跑了;判决逐行一致,详见实测——"只引用后者"改为"只需引用一份"):`results/skill-scanner/<日期>/`(全量 3,539)和 `results/skill-scanner/<日期>-excl-cisco/`
      (`--exclude-source cisco-mcp-scanner-evals`,3,412)。对照表里 Cisco 那列**只引用后者**,
      MCP 行写"不可测(干净 n=3)"。
- [x] **探针先于全量**:五个探针的结果写进设计文档「实测」节 —— 一个恶意 skill、一个良性 skill、
      一个 `.mcp.json` 样本、一个空目录、`sg-bytecode-poisoning` 单独一次 —— 全量之前不动
      `tools.yaml` 的 `executes_scanned_content`。
- [x] `make verify` 绿(2026-09-25,`verify: all gates passed`)。

## 不做什么

每条在 review 包里要能给出「确实没动」的证明(`git diff --stat origin/dev -- <路径>`):

1. **不动 `baselines/ledger`、`baselines/tripwire`、`baselines/isolate`、`baselines/corpus`、
   `baselines/adapter/sarif`。** 中立那半和共享折叠都不动;Cisco 的 SARIF 已知**不填 artifacts**
   且 level 映射与 cc-audit 同形,`ArtifactsNotEmitted` 现成。
2. **不动 `internal/**`。** 测出来的漏报进 `work-items`,修法另开。
3. **不动 `baselines/adapter/ccaudit`、`baselines/adapter/heeler`。** `pick()` 只加一个 case。
4. **只用 `skill-scanner scan <dir>` 一个入口,默认档。** 不用 `--use-llm` / `--use-behavioral` /
   `--use-aidefense`(出网或非确定)、`--use-virustotal --vt-upload-files`(上传)、`scan-repo`
   (克隆)、`scan-all`(它自己的遍历替代了我们的工作表)。
5. **`--lenient` 若跑,单独目录、不进对照表头行。** 它测的是一个用户默认拿不到的模式。
6. **不改 `direction.zh-CN.md`、`competitor-research-2026-09.zh-CN.md` 的结论**,不写
   `audit-log.zh-CN.md`。
7. **语料 checkout 扫完必须干净**:`git -C ../agent-artifact-corpus status --porcelain` 为空。
   Cisco 的 `scan` 不写文件,但 `--policy` 的 dump 子命令会,不在语料目录里用。

## 不能说什么

1. **不能用全量分母给 Cisco 的 MCP 面报任何数。** 130 个恶意 MCP 里 127 个是它自己的评测集;
   干净分母 n=3。对照表 MCP 行只能写"不可测"。
2. **不能说赢或输。** `provisional: true`。它公开了自己的 P/R 并自判不及格 —— 那是它的数,
   我们的数是我们替它测的。
3. **不能拿 `--lenient` 比我们的默认档**,也不能拿 `--policy strict` 比。默认对默认。
4. **不能把 exit 1 说成"它崩了"** —— 也**不能把 exit 1 说成"它响了"**。对它两者同码,
   要看 stdout/stderr 才知道是哪个。
5. **不能说"它不外连"**,只能说"默认档在文档里零联网,未在 `--network none` 下测量"。
6. **不能把它的 0–100 分当风险分引用。** 方向是反的:它量的是"我看懂了多少",不是"多危险"。
7. **不能说"它执行了被扫内容"**,也不能说"它没有" —— 说事实:`marshal.load()` 在 magic/flags
   校验后反序列化被扫 `.pyc`,CPython 文档写明该原语不防恶意数据;`compile()` 只编译不执行。

## 工作项

一条一个提交:

| W | 一句话 | 提交信息(不写 sha) |
|---|---|---|
| 1 | 先写测试跑红:`pick` 派发、exit 1 不等于闸门、无 SKILL.md 是 unsupported-input | `baselines: three red tests for a scanner whose exit 1 means two things (P-022)` |
| 2 | 钉版本装进 venv;五个探针;结果写进「实测」节,定 `executes_scanned_content` 与 lenient 的去留 | `proposals: P-022 probes — five runs before the 3,539 (P-022)` |
| 3 | `adapter/cisco`:调用、stderr 感知的 classify、`Placement()`、`Version()` | `baselines: point the rig at cisco, and read its exit 1 by what it printed (P-022)` |
| 4 | `pick()` 加 case;`tools.yaml` 加条目(`uploads_samples_basis` 以 NOT MEASURED 开头) | `baselines: register cisco with the facts and the non-facts (P-022)` |
| 5 | `FixtureRunner`:六个 fixture,判据取语料 `Expected` | `baselines: the six fixtures, judged by the corpus's own sentences (P-022)` |
| 6 | 两个分母各跑一次,两个结果目录;反向断言 | `baselines: two denominators for a scanner that wrote part of the exam (P-022)` |
| 7 | 语料仓:`taxonomy/tools.yaml` 加 `cisco` + 按提升度测出的 `dimension_map`;回填 `row.Dimensions` | `taxonomy: register cisco, with a dimension_map the evidence chose` / `baselines: cisco names 17 kinds; five of ours (P-022)` |
| 8 | `baselines/README.md` + `corpus-benchmark.zh-CN.md` 的口径:三列怎么读、MCP 行为什么空 | `baselines: say what three columns mean, and which cell is honestly empty (P-022)` |

## 未决问题

design 时一次问完,每条给建议答案。人答了写"**已决(日期)**:…",不删。

**1. 没有 `SKILL.md` 的四个面(mcp / hooks / permission / instruction / connector)怎么处理?**
默认(strict)档下 loader 抛 `SkillLoadError`、exit 1、不出 SARIF。`--lenient` 会回落到扫目录里的
`.md` 文件,但那是用户默认拿不到的模式。
- **建议:默认档做头行,这些面记 `no-verdict / unsupported-input`,detail 带 loader 原话。**
  这正是 `ledger.UnsupportedInput` 存在的理由(「它拒绝这个输入形状」),也是
  `surfaces_declared_uncovered` 第一次拿到带证据的声明的机会。
- 可选:再跑一份 `--lenient` 进 `<日期>-lenient/`,**不进对照表头行**,只在正文里说"开了 lenient
  之后 X 个面从不可测变成可测,数字是……"。
- 替代:头行就用 `--lenient`。反对理由:那不是它的出厂行为,「测的行为必须是发出去的行为」。


**已决(2026-09-24)**:默认档做头行,这些面记 `no-verdict / unsupported-input`,detail 带 loader 原话。`--lenient` 若跑进单独目录,不进头行。
**2. 两个分母怎么产出?**
- **建议:driver 跑两次,`-samples` 分别给全量工作表和 `corpus samples --exclude-source
  cisco-mcp-scanner-evals` 的工作表,两个结果目录。** `run.yaml` 的 `work_list_size` 会自己说明
  分母是多少,不需要新机制。
- 替代:教 `corpus score` 认 `--exclude-source`。多一个机制,且语料侧的活。


**已决(2026-09-24)**:按建议,driver 跑两次,两个结果目录。
**3. `executes_scanned_content` 填什么?**
`bytecode_analyzer.py:260` 对被扫 `.pyc` 调 `marshal.load()`,前面校验 magic number 与 flags;
`:200` 对配对的 `.py` 调 `compile()`(编译,不执行)。语料里恰有一个 `.pyc`
(`sg-bytecode-poisoning`,skillsgoat,truth `exfiltration/critical`)。
- **建议:`false`,`uploads_samples_basis` 同款写一段 basis 说明 marshal 那条;W2 先单独探
  `sg-bytecode-poisoning` 一次,看它是否崩、是否报、报什么。** 反序列化不是执行,但离得不远,
  所以把事实写在行上比选一个 bool 更要紧。
- 替代:`true`。后果:driver 会拒绝运行(没有容器路径),本条整个做不了。


**已决(2026-09-24)**:`false`,basis 写清 `marshal.load()` 那条;W2 先单独探 `sg-bytecode-poisoning`。
**4. 阈值取哪一档?**
它的 `--fail-on-findings`(legacy)= `--fail-on-severity high`;SARIF 把 critical/high 都映到 `error`。
- **建议:`tools.yaml` 写 `threshold: high`(它自己的阶梯,critical/high/medium/low/info/safe),
  adapter 传 `--fail-on-severity high`,SARIF 折叠在 `error`。** 三处一致,且等于它文档里的默认门禁。


**已决(2026-09-24)**:按建议,`threshold: high`,`--fail-on-severity high`,SARIF 折叠在 `error`。
**5. policy 怎么记?**
内置默认叫 `balanced`(`scan_policy.py:81`),随包发,不是我们手上的文件;driver 的 `-policy`
要一个文件来哈希。
- **建议:W2 用它的 dump 子命令把内置默认 policy 导出成文件,`-policy` 传它,哈希进 `run.yaml`。**
  这样"默认档"变成一个可复算的哈希,而不是"2.1.0 当时的默认"。**导出不在语料目录里做。**
- 替代:不传 `--policy`,`run.yaml` 只记 "balanced (built-in, 2.1.0)"。少一步,但哈希没有。


**已决(2026-09-24)**:用 dump 子命令导出默认 policy,`-policy` 传它、哈希进 `run.yaml`;W2 先验证导出件与不传 `--policy` 行为一致。导出不在语料目录里做。
**6. `dimension_map` 怎么定?**
17 个 `ThreatCategory`。按名字写至少有六七条是判断题(`unauthorized_tool_use`→permission?
`malware`→backdoor? `tool_chaining_abuse`→exfiltration? `transitive_trust_abuse`→injection?)。
- **建议:P-017 的方法,一字不改:首轮全量之后,在 read 基的被抓样本上按提升度
  `P(维度|category)/P(维度)` 交叉制表,≥1.5 才映射,其余留空并写明为什么。** 映射归语料仓
  `taxonomy/tools.yaml`,adapter 里手抄一份。**不在首轮之前定**,首轮 attribution 因此会是
  那个已知的假 0%(scorer 缺陷未修),结果目录先不提交,和 P-017 当时一样。


**已决(2026-09-24)**:按建议,首轮之后按提升度定,归语料仓 `taxonomy/tools.yaml`。
**7. 怎么装、怎么钉?**
PyPI `cisco-ai-skill-scanner==2.1.0`,`requires-python >=3.11,<3.15`(本机 3.12.0);
arm64 wheel sha256 `452811a6…852bf6`;33 个直接依赖;wheel 带平台标签(有编译产物)。
- **建议:`uv venv` + `uv pip install --require-hashes`,`run.yaml` 记 `skill-scanner --version`
  的输出与 wheel 哈希。** pipx 不给你一个能写进 `run.yaml` 的哈希。


**已决(2026-09-24)**:按建议,`uv venv` + 钉 wheel sha256。
**8. 数出来它在 skills 面上赢我们,怎么办?**
- **建议:照实提交,叙事另开** —— P-017 已决的口径,不再问第二次。这里只确认。


**已决(2026-09-24)**:照实提交,叙事另开。

## 实现阶段追加的未决问题

**9. 提升度算不出 `dimension_map`——已决 6 的前提不成立。**(2026-09-24,首轮 raw 之后)

P-017 在 159 个带 category 的 read 基恶意样本上按提升度定映射。这里同样的脚本跑出来:

```
read-basis 恶意样本 265 · 其中被抓(HIGH+)且报了 category 的 23 · 维度标记 29

command_injection     出现在 10 个样本    execution     n=3  lift 1.09
obfuscation           出现在  7 个样本    execution     n=5  lift 2.27 ←   exfiltration n=3 lift 1.55 ←
data_exfiltration     出现在  7 个样本    execution     n=6  lift 2.42 ←
prompt_injection      出现在  4 个样本    exfiltration  n=3  lift 2.07 ←   injection    n=3 lift 1.81 ←
policy_violation / supply_chain_attack / hardcoded_secrets / malware   n<3,测不了
```

**23 个样本撑不起任何一条映射。** `data_exfiltration` 的最高提升度指向 `execution`(n=6)——按这个
写会把它明明说对的 exfiltration 记成说错。原因是结构性的:strict 档只扫 skills 面,127 个 MCP
恶意样本(它 category 最可能丰富的地方)全被拒绝;skills 面上它 HIGH+ 的恶意样本本来就少。

三条路,每条都有代价:

- **(a) 按名字写,并在语料 `taxonomy/tools.yaml` 里逐条标"by name — n too small to measure"。**
  Cisco 自己公开了 category → 威胁分类的映射表(`docs/architecture/threat-taxonomy.md`),
  名字不是我们发明的。代价:违反 P-017 立下的"不按名字"——那条规则救过两条错映射。
  五条名字无歧义:`prompt_injection`→injection、`data_exfiltration`→exfiltration、
  `command_injection`→execution、`supply_chain_attack`→supply-chain、`resource_abuse`→resource-abuse。
  其余 12 个留空并写明"未测、未按名字猜"。
- **(b) 不注册 `cisco` 条目,attribution 那行标"未测量"。** 最诚实,但 `corpus score` 的缺陷
  (P-017 未决 10)会把它印成 `0 of N correct (0%)`,和"每类都说错"无法区分——那条缺陷还没修。
- **(c) 先跑 `--lenient` 拿更多 category,再算提升度。** 能把 MCP 面的 127 个拉进来。代价:
  用一个非出厂模式的数据去定一张会用在出厂模式成绩单上的映射;而且 P-022 已决 1 说 lenient
  不进头行。

**建议 (a)**:五条无歧义的按名字、逐条标注、其余留空;设计文档和 `tools.yaml` 都写明
"这张表没有 P-017 那种证据,语料长到能测时重算"。理由:(b) 会发布一个假 0%,(c) 混了口径。

**已决(2026-09-24)**:(a)。五条按名字,语料仓与 adapter 里每条都标 "by name — n too small to
measure";其余 12 条留空并写明"未测、未猜"。**这张表的证据等级低于 P-017 的,成绩单要带这句。**

---

**八条全部已决(2026-09-24),设计已接受,进实现阶段,不合主干。**

## 完成

```
合入:PR 待合入(2026-09-27;sha 合入后用 git log origin/dev --grep 'P-022' 找)
发布:v0.13.0
证据:TestAnExitOfOneIsNotAlwaysTheGate、TestNoSkillMdIsUnsupportedInputNotAMiss
     (baselines/adapter/cisco/cisco_test.go);TestPickDispatchesToCisco、
     TestPickRefusesCiscoWithoutAPolicy(baselines/cmd/baseline/pick_test.go);
     账本 2642 scored · 897 no-verdict · 0 error = 3539 of 3539,897 个全部 unsupported-input
     带 loader 原话(baselines/results/skill-scanner/2026-09-24/ledger.jsonl);
     恶意 flagged 27/300(它 scored 的 143 个 read 基里 25),良性 76/3220,hard negative 1/4 被评,
     attribution 11/27 = 41% [25,59](映射按名字,证据等级低于 P-017)
     = 三家里最克制、召回最低,且四个面上的零是拒绝不是漏;
     tripwire 两次拦对(mcp 0/130 → 声明;permission 1/6 → stale,声明收回);
     反向断言:origin/dev worktree 的 aguard 与本分支同一工作表 verdicts.jsonl cmp 逐字节相同,两边 87/300;
     第二分母(3,412)与全量在共同样本上判决零差异(它连自己的评测集都没打开)
```
