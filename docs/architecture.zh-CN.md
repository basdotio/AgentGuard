# AgentGuard 架构(as built)

[English](architecture.md) | **中文**

给刚接手这套代码的人看的地图。它描述的是**代码今天是什么样**,而不是应该是什么样 —— 这个区分决定
了两份文档冲突时该听谁的:

| 文档 | 权威性 | 位置 |
|---|---|---|
| [`docs/spec/spec.zh-CN.md`](spec/spec.zh-CN.md) | **规范性。** 什么必须成立。源码注释以 `spec §N` 引用它。 | 本仓 |
| [`README.md`](../README.zh-CN.md) | 面向用户:安装、命令、扫什么、能力边界。 | 本仓 |
| [`ROADMAP.md`](../ROADMAP.md) | 什么已完成、什么被推迟,以及**诚实的已知局限**(含已确认的规避手法)。 | 本仓 |
| [`rules.md`](rules.md) | **生成的**(`make docs`,CI 校验):每一个规则 ID 及其维度、严重度、触发原因。 | 本仓 |
| **本文** | 描述性:as-built 地图。**改动它描述的结构时,在同一个 PR 里一起改。** | 本仓 |

规模,供校准:16 个 `internal` 包加 CLI,非测试 Go 约 2 万行,测试约 2 万行(596 个测试函数
外加 2 个 fuzz 目标),三个直接依赖(`spf13/cobra`、`gopkg.in/yaml.v3`、`golang.org/x/term`),单个静态二进制,不用 CGO。

## 一段话讲清心智模型

AI 编码 agent 会**自动加载** skills、MCP server、hooks、subagents、slash commands、指令文件。
这些东西是**"指令 + 被授予的工具权限"**,不是文档 —— 其中任何一个都能藏一条 agent 会照做的指令。
AgentGuard 把这些 artifact 从磁盘上读进来跑规则,产出一个可复现的风险分和一份报告。它**从不执行
被扫描的内容,也不外连**(除非你显式打开可选的 LLM judge)。它是**体检,不是保证清除**:分数是
相对风险信号,不是安全证书。

## 流水线

[`cmd/aguard/main.go`](../cmd/aguard/main.go) 里的 `analyze()` 是 `scan`/`check`/`clean` 的
**唯一编排入口**。**各阶段的先后顺序是有意为之**,该函数的注释解释了原因:

```
collect  → detect → permcheck → reputation → ignore/baseline → judge(可选) → hygiene → score → report
(§4)       (§5.1)   (§7)         (D11)        (.aguardignore)   (§5.2)        (§6)      (§5.3)  (§9)
```

其中四处是**决定**,不只是顺序:

- **reputation 在 baseline 之前** —— 先用内嵌白名单压掉可信工具自身的噪声,基线文件就只需要覆盖
  真正剩下的部分。
- **judge 在抑制之后** —— 这样即使是 known-good 的 artifact,旁注仍能浮出来。
- **抑制在评分之前** —— 基线会改变分数,所以"每次抑制必须留一条带**最高被抑制严重度**的 note"
  是硬要求。
- **`check` 的入口是布局路由,不是 `scan`** —— `collect.CollectTarget` 按"从具体到宽泛"依次判定:
  单文件 → `SKILL.md` → plugin manifest → 像 root → 其他目录(整棵树当一个 artifact 读掉)。

每个包都是围绕 [`internal/model`](../internal/model/model.go) 中不可变类型的一个(近似)纯函数阶段。

## 包地图

| 包 | 职责 | 行数 |
|---|---|---|
| [`collect`](../internal/collect/) | 枚举并读取 artifact;canonical 哈希;符号链接边界;认领 root 的"无人认领那一半" | 3014 |
| [`detect`](../internal/detect/) | 规则引擎:面向单行的正则规则(条数与清单以 [`rules.md`](rules.md) 为准)、结构化与形状检查、文件角色、逻辑行归一化、注释感知、脱敏 | 3237 |
| [`judge`](../internal/judge/) | 可选的 LLM 各趟、nonce barrier、证据落地、k-of-n 共识 | 1980 |
| [`report`](../internal/report/) | 终端 / JSON / SARIF / 自包含 HTML 渲染器;控制字符与 bidi 清理(与 `detect` 共用一个分类器) | 2032 |
| [`hygiene`](../internal/hygiene/) | 垃圾分析:重复、上下文膨胀、失效引用、僵尸 skill | 1054 |
| [`permcheck`](../internal/permcheck/) | 权限白名单体检,含可逃逸二进制表 | 262 |
| [`model`](../internal/model/) | 各阶段共享的不可变结果类型 | 613 |
| [`config`](../internal/config/) | 配置文件加载、预设、密钥文件与端点规则(LLM judge) | 452 |
| [`score`](../internal/score/) | 确定性评分 + LLM 的单向升级 | 142 |
| [`ignore`](../internal/ignore/) | `.aguardignore` 基线抑制 | 184 |
| [`clean`](../internal/clean/) | 僵尸 skill 的可逆隔离(`--apply` / `--undo`) —— 审计手册见 [`clean-internals.md`](internals/clean-internals.md) | 2005 |
| [`parse`](../internal/parse/) | SKILL.md front-matter / settings 解析 | 111 |
| [`reputation`](../internal/reputation/) | 内嵌、版本化的信誉名单,按 canonical hash 索引 | 120 |
| [`gate`](../internal/gate/) | 加载时闸门:hook 事件分派、判决、按哈希的批准、`settings.json` 合并安装与双槽备份 | 1646 |
| [`inbox`](../internal/inbox/) | 下载目录扫描:候选发现、带上限且拒绝路径穿越的 zip 解包 | 376 |
| [`safeio`](../internal/safeio/) | 打开一个不是自己写的文件的唯一途径:非常规文件在 Open 前拒绝,大小由读强制 | 101 |
| [`cmd/aguard`](../cmd/aguard/) | CLI 接线、`analyze()`、下载目录那一趟、hook runner、`llm setup/test/status` | — |

有一处职责是被刻意切成两半的,值得先知道,因为两半都叫"permission":

- **`permcheck` 只看 allow 条目的*文本形状*** —— 它从不开文件。
- **`detect.permissionUnits` 负责跟进 allow 项引用的本地脚本。** `Bash(./scripts/deploy.sh *)`
  这条授权有多危险,取决于 `deploy.sh` 干什么 —— 那是"把文件读进来跑规则",不是语义判断,所以是
  **静态不是 AI**。

## 不变量

这些是本工具可信的根据。其中多条在**不止一处**被强制执行,改动时必须**全部**一起改。完整原文和理由
在容器层的 `.claude/rules/invariants.md`(2026-09-16 从 `CLAUDE.md` 搬出)和各包的包级 doc 注释里;简表:

1. **绝不执行被扫描内容,绝不外连**(除显式开启的 LLM judge)。全程只读。
2. **符号链接边界收敛,出错即拒(fail-closed)。** `collect.withinDir` 与 `detect.inBoundary`
   都会**先解析**符号链接再判断。skill *内部*文件不得指向 skill root 之外;skill 目录*本身*是
   符号链接属于合法安装方式,但解析后必须落在 `$HOME` 之内。解析失败一律拒绝。
3. **脱敏只有一个收口。** `detect.Redact` 是产出 snippet 的唯一途径,并且在所有地方都**先脱敏
   再截断** —— 否则跨越字节上限的 secret 会以残片形式漏出去。报告与 LLM judge 消费同一份已脱敏视图。
4. **两个分数:`Overall` 纯确定性,`OverallEffective` 含 LLM 单向升级。** 单向性**由公式结构保证**,
   不靠约定:`OverallEffective = min(Overall, 升级分)`。于是一次成功的注入只能让攻击者自己的
   artifact 更可疑 —— **攻击收益为负**。
5. **任何遗漏都不许静默。** 每一处覆盖缺口或抑制都要产出一条**永不计分**的 dimension-0 note:
   `IO-000` `COV-000` `PARSE-000` `SCOPE-001` `IGN-000` `REP-GOOD` `LLM-000` `LLM-002`。抑制类
   note 必须携带**被抑制项中的最高严重度**。
6. **并发不得改变输出。** `detect.Engine.Run` 按 artifact 并发,但结果按下标回写,因此 finding
   顺序与串行执行完全一致。
### 终端报告的两种模式

默认模式打印**全部**发现,并把维度 0 的覆盖度说明折成一行 —— 这行带着条数、其中的最高严重度
和规则 ID;`--verbose` 把这些说明的正文完整打出来。分这两档的理由是:覆盖度说明的正文长是
必然的(它要讲清楚**没读**什么、为什么),而真机上这一段的体量压过了它下面的发现本身,读者于是
学会了跳过报告的结尾。

**两种模式下发现都不折叠**,被折的只有维度 0 的元信息。这个拆分最容易滑向的失败正是"默认视图
少报" —— 而一条从没被展示过的发现,读者不可能想到去追问它的细节。不变量 #5 在折叠后依然成立,
因为严重度跟着条数一起走:一条压掉了 critical 的基线不会被读成低危脚注。`--json`、`--html` 和 `--md`
不受这个开关影响,所以 CI 的输出不会取决于某个人选了哪种模式。`--md`(P-008)是第三个人读渲染器,给 PR 评论
和 issue:同一份派生数据、同一顺序,被扫目录来的字符串全在代码跨度里。

7. **终端渲染器要清掉控制字符**(`report.sanitize`)—— finding 里带有攻击者可影响的文件名,以及
   (开 `--llm` 时)模型生成的文本。JSON 和 HTML 由各自的编码器转义。

这些不变量要防的失败模式**只有一个形状:什么都没读到的扫描,渲染成了一张健康证明。**
`check <打错的路径>`、被路由到 `CollectAll` 的裸目录、root 里无人认领的那一半、打错的 `--root` ——
在本项目历史上**每一个都曾输出 `100/100` + 退出码 `0`**。所以动采集相关的代码时,先问一句:
**空结果会渲染成什么?**

## 评分

```
单 artifact = clamp(100 − Σ_维度 max(严重度惩罚), 0, 100)   # critical 40 · high 25 · medium 12 · low 5
总分        = 各 artifact 分数的平均值,再套木桶封顶: 出现 critical → ≤49 · 出现 high → ≤69
风险等级    = ≥85 Low · ≥70 Watch · ≥50 Elevated · <50 High
```

同一维度内只取最高的那一次命中,跨维度惩罚相加。**放规则时会刻意利用这个性质** —— 例如 `OBF-004`
落在维度 6 而不是 3,就是为了让惩罚相加而不是被吸收掉。

**可复现是硬要求**(这个分数将来要喂给上链 attestation),因此任何非确定性的东西都不能进这个数。
十个维度:1 注入 · 2 过度权限 · 3 数据外泄 · 4 代码执行 · 5 供应链 · 6 混淆 · 7 后门(advisory)·
8 资源滥用(advisory)· 9 文件系统 · 10 意图不符(仅 LLM)。**0 = 扫描/覆盖率 note,永不计分。**

## 检测引擎要点

每条规则的 ID、维度、严重度、触发原因都编进了 [`rules.md`](rules.md),它从 `builtinRules()` 生成 ——
加规则之后跑 `make docs`,否则 CI 会红。读 [`internal/detect/`](../internal/detect/) 之前先装进
这四个概念:

- **`fileRole` 决定哪些规则会跑**,它是**最主要的误报控制手段**。随包的 `.md`(`roleDoc`)只跑
  维度 1;`SKILL.md`/`CLAUDE.md` 和 slash command(`commands/*.md`,按 kind)(`roleInstruction`)和脚本(`roleScript`)跑全部;hook command
  (`roleHookCmd`)额外跑 `.hookOnly()` 的规则 —— 脚本里有管道再正常不过,hook 里才说明问题。
  **加规则前先看它。**
- **规则匹配的是"解释器会跑的那一行",证据引的是"文件里写的那一行"。**
  [`logical.go, shape.go`](../internal/detect/logical.go) 会把行连接、零宽字符、切词用的引号(`cu""rl`)
  还原成一个归一化字符串用于匹配,而证据保留原始字节 —— 文件里写的是 `cu""rl`、报告里印
  `curl …`,等于把用户自己机器上的事实告诉错了。规则**先跑 raw,没命中才看 norm**,所以归一化
  只会多报不会漏报(尤其 `INJ-004`,它存在的意义就是报告零宽字符)。
- **有些检查是结构化的,不是正则。** `EXFIL-001/002/003` 与 `OBF-004` 在**两个粒度**上累计三条腿:
  读凭证、编码、出网。**凑齐链只要两条腿**(凭证 + 出网),编码腿是**放大器**。跨文件只到
  low + advisory,因为不相关的文件本来就会各干一半。同一文件的链若所有网络目标都是回环地址,降到
  同一档,避免本机 sidecar 把环境封顶在 69。`HOOK-002`(脚本在 HOME 外)和 `HOOK-003`(HTTP hook)
  同样是结构化检查:它们给行级规则看不见的面打分。
- **advisory 的意思是"我们无法确认"。** 维度 7、8 在构造上就是 advisory,报告必须写明
  *not confirmed*。**会区分"我确定"和"我怀疑"的工具,才敢接进 CI。**

有意跳过的:非文本扩展名、>1 MiB 的文件、`collect.ExcludeFromScan` 中的目录名(`.git`、
`node_modules`、`dist` …)、以及同一 skill 树内字节完全相同的重复文件。**每一处跳过都会披露** ——
见不变量 5。`ExcludeFromHash` 从**哈希遍历和扫描遍历里同时**排除,因为 canonical 哈希是信誉库的 key、
必须在重新构建后依然稳定;对应的补偿检查是 `SUP-004`:artifact 自己**把 agent 指进**被排除目录时报它。

**规则表有版本号。** [`rules_version.go`](../internal/detect/rules_version.go) 按引擎顺序哈希"决定一条引擎规则发现的东西"——
`builtinRules()` 里每条的 ID、维度、严重度、标志和正则源码,不含标题和解释——再加一个整数 `rulesEpoch`。结构化、权限和 note
这几类检查写在代码里,覆盖它们的只有 epoch。扫描报告里叫 `rules_version`,
`aguard version` 会打印它,`rules.md` 页眉也写着它,所以改了正则不跑 `make docs` 就过不了 CI。它**只覆盖确定性检测**——
`overall` 的来源。**改 `builtinRules()` 之外的确定性检测代码时**——结构化/形状检查、角色门、词法层、读哪些文件、`permcheck`——
只要改变了某个输入产出哪些发现,或发现的 ID/维度/严重度,**同一个提交里把 `rulesEpoch` 加一。** 没有东西强制它;忘了加,
两份报告就会自称同一版规则,而产出它们的代码其实不同。LLM 判官完全不在里面——提示词、证据落地、共识、严重度钳制都算:
判官只动 `overall_effective`,怎么跑的由报告的 `judge` 摘要说明,所以改判官从不加 epoch。

## Canonical 哈希

skill 与 plugin 用树哈希(按相对路径排序 + 每个文件的 sha256);单文件用 sha256。它是信誉库的 key,
所以必须跨机器、跨 checkout 稳定。**改动哈希逻辑会让
[`reputation.json`](../internal/reputation/data/reputation.json) 里的每一条记录全部失效**,须用
`aguard hash` 重新生成。`test/` 故意**不**排除 —— payload 会藏在那里。

## LLM judge(可选,默认关)

必须同时满足配置里 `llm.enabled: true` **和** `--llm` 才会启用;`check`/`clean` 永不调用它。按
artifact 种类跑这些趟:隐藏注入、意图不符、去混淆(**只解码、绝不执行**)、跨文件串通、hook 能力、
MCP 配置、triage 标签。被扫内容按**敌对**处理:每次调用用 `crypto/rand` 生成 **nonce barrier**
把内容围成惰性数据块;nonce 生成失败时**让该次调用失败**,而不是退化成一个可被猜到的围栏。

有两条比"跑了哪几趟"更重要:

- **铁律**:judge 只能**新增** `Source=llm` 的发现和展示用的 advisory 标签,**永远不能删除、降级
  或重排一条静态发现**;`--fail-on` 只看确定性发现,任何模型输出都改不了它的答案。
- **升级资格是逐条 finding 的**:必须过**证据落地**(`judge.Ground` 回填真实 `file:line`,落不了地
  就丢弃并计入 `LLM-005`)**加 k-of-n 共识**。`llm.authority` 管的是 `--fail-on-llm` 这个闸门,
  不是分数本身。

完整参考:[`docs/llm-judge.zh-CN.md`](llm-judge.zh-CN.md)。

## 当前状态

**已完成且承重的**:10 个维度的静态检测(规则条数以 [`rules.md`](rules.md) 头部由代码生成的计数块为准,
这里再写一个数就是会过期的副本),含 `detect/shape.go` 的形状检查(空行填充、伪装压缩包、随包字节码、改包源)、
带可逃逸二进制表的权限体检、逐条 command 的 hook artifact(并跟进被引用脚本)、plugin 采集(`installed_plugins.json`、
Claude 桌面版自己的插件/skill 仓库 `collect/desktop.go`、以及沙箱的 `synced/<uuid>/` 布局)、从桌面版缓存读取的远程 MCP
connector 工具清单(`collect/connectors.go`,从不联网)与 `MCP-001..004` 投毒规则、沙箱识别(在 Claude Cloud / Cowork 里跑出的
报告顶部盖「这不是你的电脑」横幅,`collect/environment.go`)、下载目录扫描(`internal/inbox`:~/Downloads 下像 agent 的东西和 zip,
逐个单独查,永不计分)、hygiene/clean(默认只报告;`--apply` 可逆隔离僵尸 skill,`--undo` 恢复)、确定性评分、
`.aguardignore` 基线、内嵌信誉 allowlist、带证据落地 + 共识、摘录压缩取头尾的 LLM judge、终端/JSON/SARIF/HTML 报告、
两个闸门,以及三种分发:release 二进制、Claude Code 插件、`@bas.io/guard` npm 包(五个包,无 install 脚本)。
v0.9.0 起:读用户文件一律经 `safeio`;存在但读不了的条目被披露并改变树哈希;`@import` 凭据被拒并计分(`EXFIL-005`);
`check` 只按结构路由;报告在均值旁并列最差 artifact。

**刻意还没做的** —— 每一条都是有记录的决定(见 [`ROADMAP.md`](../ROADMAP.md)),不是疏漏:

- **AST 检测。** 词法那半已经做了;真 AST 要么 CGO(tree-sitter)要么每种语言一个大的纯 Go 解析器,
  与"单静态二进制 + 两个依赖"冲突。所以链式调用 `Buffer.from(secret).toString('base64')` 和跨语句
  追踪目前做不到。
- **信誉 blocklist。** 机制接好了,**内容是空的** —— 策展已知恶意哈希是持续工作。现阶段信誉库的
  价值是"给可信工具包降噪",检出靠规则。
- **云端信誉查询。** 代码里没有任何东西连托管名单;接口等第一个实现一起加(ROADMAP D.11)。
- **Windows 二进制。** 它能干净地交叉编译,而这正是陷阱:CI 只跑 ubuntu、代码里没有任何 `GOOS`
  分支、不变量 2 背后的符号链接约束依赖的原语在那边行为不同。源码构建可用;发布产物只有
  darwin/linux,直到有 CI matrix 在 Windows 上跑完整套测试。

**已确认的规避手法**在 [`cmd/aguard/adversarial_test.go`](../cmd/aguard/adversarial_test.go) 里
**反向断言** —— 补上任何一条都会让测试失败,于是缺口不会像 checklist 条目那样被忘掉。目前只剩一条:
`ExcludeFromScan` 目录(vendored 或生成的树)的内容不扫 —— 那里的发现是别人依赖的问题 —— 而把 agent
指进去的 artifact 会被报(`SUP-004`)。未知扩展名、无主顶层文件、同形字命令名曾在这张表上,现在都能抓到;
更长的经过在 ROADMAP 的「Known limitations」。

## 上手干活

```bash
make build            # -> bin/aguard(CGO_ENABLED=0,version/commit/date 由 -ldflags 注入)
make test             # go test -race -cover ./...
make lint             # golangci-lint
make dist             # 发布二进制 + SHA256SUMS.txt(darwin/linux)
go test -run TestFailGate ./cmd/aguard/     # 跑单个测试
./bin/aguard scan --root ~/.claude          # 真机冒烟测试
```

评审时会被要求遵守的约定:

- 每个 `.go` 文件开头是 `// SPDX-License-Identifier: MIT`;每个包挑一个文件写包级 doc 注释,并在
  其中声明该包负责的不变量。
- **代码和所有用户可见字符串只用英文。** 文档做双语对子(`README.md`/`README.zh-CN.md`、本文与其
  英文版)—— 改一个就要改另一个。
- 注释会引用规格(`spec §16.3`)。请延续这个风格:一处不显然的防护,应当说明它实现的是哪条要求。
- 测试是 table-driven 的,fixture 用 `t.TempDir()` 现搭(没有 `testdata/`);judge 的测试用
  `httptest`。**真正值钱的是不变量测试** —— 不执行、边界不越界、secret 已脱敏、LLM 发现不动分数、
  `--fail-on` 契约。上面编号的不变量只要有改动,就补一条对应的测试。
- 发布:推一个 `v*` tag;[`release.yml`](../.github/workflows/release.yml) 会重跑 vet + 测试、
  用 `make dist` 构建、校验烧进二进制的版本号与 tag 一致,然后连校验和一起发布。

改完 [`logical.go`](../internal/detect/logical.go) 或读取路径上的任何东西,**开 PR 前先用
`aguard scan` 扫一遍真实的 `~/.claude`**。那是一个手写 lexer,吃的是攻击者可控的字节;它的第一版
在真机上 panic 过(行尾是一个引号),而 table test 全绿。现在有两个 fuzz 目标守着它。
