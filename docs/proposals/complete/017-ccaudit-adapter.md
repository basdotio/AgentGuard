<!-- SPDX-License-Identifier: MIT -->
# 017 — 唯一和我们打平的竞品,只被测过一次:一台机器、一个样本

- **来源**:新发现(2026-09-23,对话提出);证据来自 `docs/decisions/audit-log.zh-CN.md` 条目 3(2026-09-13 真机实测)与条目 4(2026-09-14 源码级调研)
- **依赖**:无(2026-09-23 已决:`pick()` 由本条自己写,不等 P-018;见未决问题 1)
  <!-- 文中的 P-018 是 Heeler adapter。它原为 P-016,2026-09-24 因上游先推了另一条
       P-016(配置面四种形状,PR #16)而按「撞了后推的改号」改为 018。 -->
- **分支**:`p/017-ccaudit-adapter`

## 问题

**P-015 建好的量测台,到今天只产出过一列数,而那一列是我们自己。**
`baselines/results/` 底下只有 `aguard/2026-09-22/`。`docs/planning/corpus-benchmark.zh-CN.md`
把这件事说得很清楚:一个只有自己一列的对照表,不是 benchmark,是自述。

**P-018 想加第二列,加不上。** Heeler 要账号、要读 ToS、样本要上传,服务端还有
3 文件 / 单文件 250,000 字节 / 合计 750,000 字节的硬上限 —— 实测 3,539 个样本里 158 个
(4.5%)一上来就超限。分支停在 5 个提交,未推,而且**不是"还没做完",是这台机器上做不完**:
剩下的每一步都要一个我们没有的账号。

与此同时,**我们赖以立身的那一条主张,已经有人正面打平了,而我们拿出的反证是 n=1。**

`ryo-ebata/cc-audit`(Rust,MIT,24★)在 `audit-log` 条目 4 的对照表里占着 (c) 那一格:

> **`ryo-ebata/cc-audit` 正面打平**:`src/scoring.rs` 整数权重 40/20/10/5、上限 100、Rust、
> 无 LLM 依赖、无网络,**critical 权重同为 40**(极性相反,它越高越危险)。

2026-09-23 按 tag `v3.23.9` 逐行核对,**这一段今天仍然成立**:`src/scoring.rs` 的
`CRITICAL_WEIGHT: u32 = 40`、`HIGH 20`、`MEDIUM 10`、`LOW 5`、`MAX_SCORE 100`,
`RiskLevel::from_score` 分五档。

`competitor-research-2026-09.zh-CN.md:266` 和条目 4 的收尾表把结论收在同一句上——
「已装环境 + 确定性分数 + 可离线复算」是**组合**独有,因为 "Cisco 确定但无分数;
cc-audit 有分数但**覆盖窄**"。整条差异化叙事,压在"覆盖窄"这三个字上。

**而"覆盖窄"目前的全部证据是一次 n=1 的手测。** 条目 3(2026-09-13)在同一台 Mac、同一个
`~/.claude` 上跑了三家,拿到两件事:

| | 结果 |
|---|---|
| AgentGuard | `HOOK-003` high,每次会话启动都提醒 |
| cc-audit | PASS |
| medusa | 753 条 findings 里没有这一条 |

条目 3.B 的原话是「**这是一条可以直接引用的差异化证据**」。它确实是——但它是**一个样本、
一台机器、一条 hook**。语料仓里有 3,539 个,其中 300 个恶意。我们手上有一台能把这句话从
"我们见过一次"变成一个带 Wilson 区间的数的机器,却从没对它跑过。

还有一件条目 3.A 记下、之后**再没人量化过**的事:

> `cc-audit` 的 `--remote` / `--client` / `--pin --client` **三处静默失效并返回绿灯**

这在我们自己的话语体系里是**不变量 #5 那一类的缺陷**(任何遗漏都不许静默)。我们为此写了
八条 dimension-0 note、写了 `ledger` 三态划分、写了 tripwire——而唯一一个被观察到正好踩中
这条的竞品,我们只留下了一句话。

### 2026-09-23 的核对推翻了两处,方向都对我们不利

**其一,一处引用错误(我在 draft 段写的)。** 条目 3 开头那个 ⚠️「测的是 `v0.4.5`
(2026-09-07),距当时已 34 个提交」说的是**我们自己被评审时的版本**,不是 cc-audit 的。
draft 段据此写的"关于 cc-audit 的每个字都说的是十几天没人跑过的版本"**证据用错了**。
结论(那些字已过期)仍然成立,但真正的理由是相反的:

| | draft 段的说法 | `gh api` 核对(2026-09-23) |
|---|---|---|
| 当前版本 | ~v0.4.5,十天没动 | **v3.23.9**,`pushed_at` 2026-09-22 |
| 发布历史 | —— | **146 个 release**;v1.0.0 → v3.0.0 只隔一天(2026-01-25 → 01-26) |
| 结论 | 它停更了 | **它几乎一天一版**,我们那次手测之后又发了很多版 |

**其二,更要紧:"覆盖窄"这个前提本身可能已经作废。** `v3.23.9` 的 README 自述的扫描面是
Skills、hooks、MCP servers、commands、Docker、dependencies、subagents、plugins,
四个客户端(Claude / Cursor / Windsurf / VS Code),外加远程仓库扫描、CVE 库、SBOM、
proxy 运行时监控、baseline 漂移、MCP pinning,以及 "100+ 检测规则"。`src/` 下与之对应的
文件都在(`cve_db.rs` 22KB、`malware_db.rs` 34KB、`deobfuscation.rs` 38KB、`sbom/`、
`proxy/`、`remote/`、`pinning.rs`)。

我们是 **1 个 harness**。条目 4 自己就写过「`cc-audit --all-clients`(4 个)比我们宽」
「**我们是 1 个 harness,覆盖最窄**」。所以"cc-audit 覆盖窄"这句话,即使在 2026-01 成立,
今天也需要重新证明——**而它是我们整条差异化叙事的承重墙。**

这正是做这条提案的理由,也是它令人不舒服的地方:**一个只在预计会赢的时候才跑的
benchmark,不是 benchmark。** 判据里因此没有一条写着"我们要比它高"。

受影响的是三处会引用这些结论的地方:`direction`(差异化定位)、`corpus-benchmark`
(对照表口径)、以及任何对外说"我们比 X 覆盖宽"的文字。

## 初步方向

加第三个 adapter,`baselines/adapter/ccaudit/`,`tools.yaml` 加一条。中立的那半
(`ledger`、`tripwire`、`isolate`、`corpus score`)一行不动——它们不认识任何工具,
P-015 的整个设计就是为了这一刻。

**它和 Heeler 形状相反,这是它值得插队的理由**(下表"核对"列全部来自 `gh api` 读 `v3.23.9`
的 README / `docs/CLI.md` / `src/`,**未执行任何 cc-audit 代码**,与条目 4 的做法一致):

| | Heeler(P-018) | cc-audit(本条) |
|---|---|---|
| 许可 | 商业,要读 ToS | MIT(`LICENSE` 在仓库里,`license.spdx_id = MIT`) |
| 账号 | 必需 | 无 |
| 安装 | 下载 + 登录 | `brew` / `cargo` / `npm` / GitHub Releases 有 `aarch64-apple-darwin` 预编译包 + `.sha256` |
| 样本上传 | 是 | **待实测**(见未决问题 4;源码里唯一的外发路径是 `--report-fp`,它 shell out 到 `gh issue create`) |
| 放置方式 | 猜了三次全错 | `cc-audit check <PATHS>...`,**吃文件或目录,默认递归** —— 和 `aguard check` 同形 |
| 服务端上限 | 3 文件 / 250,000 B / 750,000 B;158 个样本超限 | 无(本机进程) |
| 今天能跑几个样本 | 0 | **预期 3,539 全部** |

**最省事的一处**:它支持 `--format sarif`。P-018 已经写好的 SARIF 折叠
(`fold.go`,含 §3.27.9 `kind` 默认 `fail`、§3.27.10 `level` 回退链、
"读到零结果不等于干净"那条)**语义上直接可用** —— 怎么复用是未决问题 1。

**一件只有它能做的事**:它的分数和我们同构(0–100 整数权重,极性相反)。
**两个确定性分数在同一份语料上逐样本对齐**,是这份语料能支撑、而任何自述都支撑不了的比较。

## 完成的判据

- [x] **每个样本都有一行**:`baselines/results/ccaudit/<日期>/ledger.jsonl` 行数 == 工作表长度
      (3,539),且 `ledger.Check(rows, workList)` 返回零 error。这是 P-015 的 ledger 不变量
      第一次在**非我们自己写的工具**上被实证——`scored` / `no-verdict` / `error` 三态必须
      划分完整个工作表,没有 `skipped`。
- [x] **反向断言:接第二个工具不许改到第一个工具的答案。**
      同一棵树上重跑 aguard,与 `origin/dev` 上同一次运行 `cmp`
      **逐字节相同**,退出 0。(这条是 P-018 欠下、语料仓编译不过时跑不了的那条;
      2026-09-23 核实 `harness/` 已能 `go build ./...`,现在跑得了。)
- [x] **`TestPickDispatchesToCCAudit`** 钉住:`pick()` 给 `-tool ccaudit` 返回
      `*ccaudit.Adapter`;给未知 tool 的错误信息同时点名"没有 adapter"和"二进制没装"两种
      可能(沿用 P-018 `pick()` 的既有约定)。
- [x] **`TestEmptySarifRunIsNotACleanBill`**(cc-audit 版)钉住:一次 SARIF 输出里
      `results` 为空**且** artifacts 为空时,判为 `no-verdict` + `NoOutput`,不是 benign。
- [~] **隔离下的网络事实被记下来,不论哪个方向。**(改判见未决 9:容器执行另开。
      代偿:`run.yaml` 新增 `uploads_samples_basis`,cc-audit 那条以 "NOT MEASURED." 开头)
      在 `isolate` 的容器里(`--network none`)跑完全量:
      - 若跑通且 ledger 的 `error` 计数为 0 → `tools.yaml` 写 `uploads_samples: false`,
        证据是这次运行本身;
      - 若跑不通 → `uploads_samples: true`,附上失败的样本与 stderr 首行。

      **两个结果都算判据达成。** 这是一次测量,不是一次期望;把"预期为 false"写进判据,
      就是 P-018 上猜 `heelercli` 调用方式那个错误的重演。
- [~] `make verify` 绿。—— 只剩一条失败且 `dev` 上同样失败:`TestDocsRelativeLinksResolve`
      的 27 条断链全在 gitignore 掉的 `_gitlocks/` 里,那个测试走文件系统而不是 git 索引。

## 不做什么

每条在 review 包里要能给出"确实没动"的证明(`git diff --stat origin/dev -- <路径>`):

1. **不动 `baselines/ledger`、`baselines/tripwire`、`baselines/isolate`、`baselines/corpus`。**
   它们是中立的那半。要是接第二个工具必须改它们,说明 P-015 的边界划错了,那要单开一条,
   不在这里顺手改。
2. **不动 `internal/**` 的任何检测代码。** 本条不修 aguard 的任何漏报误报——哪怕测出来一堆。
   发现进 `work-items`,修法另开。
3. **不碰 `p/018-heeler-adapter` 分支**,不改 `baselines/adapter/heeler/`。
4. **只用 `cc-audit check <dir>` 这一个入口。** 不测 `--remote` / `--remote-list` /
   `--awesome-claude-code`(要出网、要克隆别人仓库)、`--all-clients` / `--client`
   (读的是本机真实的 `~/.claude`,那不是语料)、`serve` / `proxy`(常驻进程)。
5. **不用任何会写文件的开关**:`--fix`、`--fix-dry-run`、`--baseline`、`--save-baseline`、
   `--pin` / `--pin-update`、`--save-profile`、`hook init`。语料 checkout 必须扫完仍是干净的
   ——判据外加一条 `git -C <语料仓> status --porcelain` 为空。
6. **不改 `direction.zh-CN.md` 和 `competitor-research-2026-09.zh-CN.md` 的结论。**
   本条只负责把数拿出来;差异化叙事要不要改,是拿到数之后的另一次判断(见未决问题 7)。
7. **不写进 `audit-log.zh-CN.md`。** 它 append-only、写下即冻结,而本条的数还没跑出来。

## 不能说什么

1. **在数出来之前,"cc-audit 覆盖窄"这句话不许再出现在任何文档、README、对外材料里。**
   它的出处是 2026-01 的调研,而 `v3.23.9` 自述的扫描面比我们宽。继续引用它,是拿一个
   我们自己刚刚证明可能已作废的前提当论据。
2. **不能说我们"赢"或"输"。** `tools.yaml` 的 `provisional: true` 就是为这个存在的:
   我们替别人做的测量不是别人自己的测量。语料仓是公开的,它的 guide 明写邀请任何厂商自测;
   在他们自测之前,我们的数是**目前可得的最好猜测**,不是他们的数。
3. **不能拿调过档的它去比默认档的我们。** 不加 `--strict`,不动 `--min-severity` /
   `--min-rule-severity` / `--min-confidence`。默认对默认——理由和 aguard 那条
   `threshold: high` 的注释一样:**测的行为必须是发出去的行为**。
4. **不能把它的 exit code 1 说成"它崩了"。** 和我们一样,1 是闸门响了,不是运行失败。
   只有非 0 非 1 才是 `Errored`。
5. **不能说"它没有网络"** —— 只能说"在 `--network none` 的容器里跑完了全量"。
   前者是对它代码的断言(我们只读了一部分),后者是我们真做过的事。

## 工作项

一条一个提交:

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 先写测试跑红:`pick` 派发、空 SARIF run 不是干净、ledger 全覆盖 | `baselines: three red tests for a tool that has no adapter yet (P-017)` |
| 2 | `pick()` 派发(**逐字对齐 P-018 的那份**,冲突留成机械可解) | `baselines: the driver can name a second tool (P-017)` |
| 3 | SARIF 折叠按未决问题 1 的答案落位 | `baselines: sarif folding stops belonging to one vendor (P-017)` |
| 4 | `adapter/ccaudit`:调用、放置、版本、判决 | `baselines: point the rig at cc-audit (P-017)` |
| 5 | `tools.yaml` 加一条,含实测得到的 `uploads_samples` | `baselines: register cc-audit with the facts the run produced (P-017)` |
| 6 | 隔离容器里跑全量,提交 `results/ccaudit/<日期>/`;重跑 aguard 做反向断言 | `baselines: the second column, and proof the first one did not move (P-017)` |
| 7 | `baselines/README.md` + `corpus-benchmark.zh-CN.md` 的口径 | `baselines: say what two columns mean and what they do not (P-017)` |

## 未决问题

design 时一次问完,每条给建议答案。人答了写"**已决(日期)**:…",不删。

**1. P-018 的 SARIF 折叠代码怎么复用?**(唯一会波及别的分支的一条)
`fold.go` 现在在 `baselines/adapter/heeler/`,而 cc-audit 也出 SARIF。
- **建议:在 P-017 里新建 `baselines/adapter/sarif/`,把 fold 抽成共享包**,P-018 将来 rebase
  时删掉自己那份、改 import。理由:SARIF §3.27.10 的 `level` 回退链正是两份实现必然漂移的
  那种东西,而漂了之后两列数就不可比了——那会毁掉整张对照表的意义。
- 代价要说清:P-018 的冲突面从"`main.go` 里一段 `pick()`"扩大到"一个包整体搬家"。
- 替代:P-017 自己抄一份 `adapter/ccaudit/fold.go`。省事,但那是明知会漂还复制。

**已决(2026-09-23)**:抽成共享包 `baselines/adapter/sarif/`。**这给 P-018 留了一笔债**:
它 rebase 到 dev 之后必须删掉 `adapter/heeler/fold.go` 与 `fold_test.go`、改 import,
并在它自己的设计文档里记一句「SARIF 折叠随 P-017 落地」。P-017 的 review 包要把这笔债写明,
否则它会以"两份 fold 都还在、谁也没删"的形式活下来。

**2. 判决读 SARIF 还是 JSON?**
它两种都出。SARIF 有现成的折叠且是标准;但那个 0–100 分只在 JSON 的 `RiskScore` 结构里
(`total` / `level` / `by_category` / `by_severity`)。
- **建议:判决走 SARIF,分数走 JSON,一个样本调两次。** 多一倍进程开销,换"判决口径和
  Heeler 那列同源"+"分数对齐这件只有它能做的事"两样都要。
- 替代:只走 JSON(一次调用,但要自己写一份判决折叠,与建议 1 的理由冲突)。

**已决(2026-09-23)**:判决走 SARIF,分数走 JSON,一个样本两次调用。
**判决只由 SARIF 那次决定** —— JSON 里的 0–100 分是记录给对照用的,不参与定 verdict;
两次调用若给出矛盾的结论(SARIF 空而分数 > 0,或反之),按 `Errored` 记并把两边都写进 detail,
不许挑一个顺眼的。

**3. 阈值取哪一档?**
- **建议:默认档**,一个 `--min-*` 都不加、不加 `--strict`。写进 `tools.yaml` 的
  `threshold: default`,并在注释里写清它对应 critical+high(README 的示例输出里
  `2 errors ... (1 critical, 1 high, 0 medium, 0 low)`,与 `Result: FAIL (exit code 1)` 对上)。
  理由同「不能说什么」第 3 条。

**已决(2026-09-23)**:按建议,默认档。注释里那句"对应 critical+high"是**从 README 示例读来的
推断,不是实测** —— W4 要用一个自造的 medium-only 样本验一次:默认档下它不该被判 flagged。
验不上就以实测为准改注释。

**4. `uploads_samples` 与 `executes_scanned_content` 怎么定?**
- **建议:两项都先按 false 写,但由 W6 的容器运行来定稿,而不是由我读源码定稿。**
  我读到的是:唯一的外发路径是 `--report-fp`(`src/feedback/submitter.rs`,shell out 到
  `gh issue create`,默认 target 是它自己的仓库),我们不会用这个开关;`--no-telemetry` 的
  存在说明默认不是全静默,但我没有读完 300 KB 的 Rust,**不打算把没读完的东西写成断言**。
  容器 `--network none` 跑通与否,是能拿出来的证据。
- `executes_scanned_content`:静态 Rust 扫描器,预期 false;但**无论如何都在容器里跑**,
  因为要证第一项。

**已决(2026-09-23)**:按建议 —— 两项由 W6 的容器运行定稿,不由我读源码定稿。

**5. 钉哪个版本?**
- **建议:钉 `v3.23.9`,下载 GitHub Releases 的 `cc-audit-v3.23.9-aarch64-apple-darwin.tar.gz`,
  核对同目录的 `.sha256`,把 tag + sha256 写进 `run.yaml`。** 理由:**它一天一版**
  (146 个 release),不钉住的数三天后就没法复算——而"可离线复算"正是本条要测的那条主张。
- 不用 `brew` / `cargo install`:两者都不给你一个可写进 `run.yaml` 的确定哈希。

**已决(2026-09-23)**:按建议,钉 `v3.23.9` + sha256。

**6. 六个注入故障 fixture 要不要也对它跑?**
- **建议:跑。** `adapter.FixtureRunner` 是可选接口,正是为此留的。跑不了的照实记
  `FixtureUntestable` + 理由,不静默省略。注意 `traverse-only-dir` 那条:aguard 靠
  dimension-0 note(`COV-000`)通过,cc-audit 没有 dimension-0 这个概念,
  **判据要按"它承诺了什么"来写,不能按我们的机制来写**。

**已决(2026-09-23)**:按建议,六个都跑,跑不了的记 `FixtureUntestable` + 理由。

**7. 如果数出来对我们不利,谁来处理?**
比如 cc-audit 在 300 个恶意样本上召回高于我们(我们当前是 28%,85/300),
或者它的误报率更低。
- **建议:P-017 只负责把数放进 `results/` 和 `corpus-benchmark` 的对照表,不做解释、
  不做辩护、不调参重跑。** 叙事怎么改是另一条提案的事(会动 `direction`,那是"拿主意的人"
  的文档)。写进本条「不做什么」第 6 条已经是这个意思,这里再确认一次口径:
  **难看的数照样提交。**

**已决(2026-09-23)**:照实提交,叙事另开一条。不解释、不辩护、不调参重跑。

**8. 结果目录的日期用哪天?**
- **建议:用实际跑完那天**,和 `aguard/2026-09-22/` 一致(那次也是重跑后按新日期建的)。

**已决(2026-09-23)**:按建议。

---

**八条全部已决(2026-09-23),设计已接受,进实现阶段。**

## 实现阶段追加的未决问题

**9. 判据 5 写的事这个仓库做不到 —— 怎么改。**(2026-09-23,W5 之后发现)

判据 5 写的是"在 `isolate` 的容器里(`--network none`)跑完全量"。实现到 W6 才查清楚:

- `baselines/isolate` 只导出两个函数:`Available()`(一个拒绝检查)和 `Args()`(拼容器参数)。
- **driver 只调用了 `Available()`,从来没调用过 `Args()`。** 仓库里不存在"在容器里执行
  adapter"这条代码路径 —— P-015 自己的错误信息就写着
  「no containerised adapter exists yet; running it on the host is not the fallback」。
- 而且 `executes_scanned_content: false` 的工具连那道闸门都不会碰到。
- 本机 Docker 装了但没跑起来。

所以判据 5 不是"还没做",是**要先把容器执行这条路径建出来**,而那是一条独立的工作,
不是 P-017 顺手能带的(它会动 driver 的整个执行模型,以及"不做什么"第 1 条说了不动 isolate)。

- **建议:把判据 5 拆成两条,一条改小、一条另开。**
  1. 全量运行照 aguard 的方式在宿主机上跑(3,539 个样本,得到那一列数);
  2. 不外连这条主张,用一次**有界的容器实验**证明:`docker run --network none` 里放进
     钉住的二进制和十来个样本树(含 malicious/benign 各几个),跑通即为证据,失败即为
     `uploads_samples: true` 的证据。实验记录进 `run.yaml` 与结果目录。
  3. "把容器执行建成 driver 的一等路径"另开一条提案 —— 它的受益者不止 cc-audit,
     `tools.yaml` 的整个 `executes_scanned_content` 机制现在都是一张空头支票。
- **代价要说清**:宿主机跑的那 3,539 次调用,其不外连性由第 2 条的有界实验**推断**而来,
  不是逐样本证明的。这个区别必须写进 `run.yaml`,不能含糊。
- 替代:P-017 就地把容器执行路径建出来。范围大得多,且会和「不做什么」第 1 条撞。

**已决(2026-09-23)**:拆成两条,容器执行另开(已建 task chip)。

**10. `corpus score` 的 attribution 行对不报维度的工具说谎。**(2026-09-23,首次全量之后发现)

全量跑完,记分卡最后一行是:

```
attribution — of caught malicious with a read-basis dimension, was the KIND named:
  0 of 185 correct (0%, [0, 2])
```

**这个 0% 是本 adapter 的产物,不是关于 cc-audit 的事实。** 它没填 `row.Dimensions`。

而且这不只是我们这一次的疏忽 —— 是**语料侧的缺陷**:`harness/cmd/corpus/score.go:130` 起的那段,
`Attribution.Scoreable` 数的是**语料标签**里有多少个被抓到的样本带 `read` 基的维度,
和**工具有没有报维度**无关。于是任何不报维度的工具都会拿到 `0 of N correct (0%)`,
而这在输出里和"每一类都说错了"**完全无法区分**。它有一条 `nothing scoreable` 分支,
但只在语料侧没有可评维度时触发,不在工具侧交白卷时触发。

这正是语料自己写下的那条纪律的反面:「a tool with no reachable reference **downgrades to
"rule ids not checked for this tool" and says so**, rather than silently passing everything」。
attribution 这一项没有照做,而它会误标此后每一个被测的第三方工具。

- ~~**已决(2026-09-23)**:本轮**不填** `Dimensions`,attribution 标成"未测量"~~
  **改判(2026-09-23,同日):映射做掉。** 那三个"判断题"不必是判断题 —— 语料自己有答案,
  只是没人去问:在 159 个 read 基、cc-audit 报了 category 的恶意样本上,把它的 category
  与真值维度交叉制表,按**提升度** `P(维度|category) / P(维度)` 排序。
  **不能看原始占比** —— 它全语料报了 5,612 条 `supplychain`,于是这个 category 和什么都
  共现,最大重叠是 `execution` 纯粹因为 `execution` 常见。第一遍我按名字写的
  "四种直接对应"里,`supplychain` 和 `overpermission` 两条在原始占比上都不成立,
  是提升度把它们救回来的。

  | category | 最高提升度 | 映射 |
  |---|---|---|
  | `persistence` | backdoor **4.33** | `backdoor` |
  | `overpermission` | permission **3.42** | `permission` |
  | `promptinjection` | injection **2.83** | `injection` |
  | `supplychain` | supply-chain **2.38**(execution 2.28) | `supply-chain` |
  | `exfiltration` | exfiltration **1.96** | `exfiltration` |
  | `obfuscation` | execution 2.32 / exfiltration 1.87 | 留空 |
  | `privilegeescalation` | 最高 1.50(n=9),分散 | 留空 |
  | `secretleak` | read 基上仅 1 次 | 留空 |

  三条留空按语料自己的规矩是**结果不是缺口**。`privilegeescalation` 是其中最有意思的:
  它报在 159 个里的 61 个上,比任何其他 category 都多,却没有一个维度过线 ——
  一个这么常见却几乎不指示攻击类型的 category,正是 part 2 要暴露的东西。
  按名字把它映到 `permission`,等于替它认领一个证据说它没说出口的类别。

  归宿是语料仓的 `taxonomy/tools.yaml`(已加 `ccaudit` 条目,`make validate` 通过),
  adapter 里那份是手抄副本,与 aguard 的做法一致。
- ~~**已决(2026-09-23)**:结果目录本轮不提交~~ **已提交**:理由消失了。

### attribution 的结果

```
cc-audit   72 of 185 correct (39%, [32, 46])
aguard     51 of 87  correct (59%, [48, 68])
```

**两个区间不重叠。** 这是整份对比里第一条我们明确赢、而且赢在质量轴上的:
它抓到的比我们多一倍多,但抓到之后说得清是哪一类的比例明显比我们低。
语料设计 part 2 就是为了这个 —— 「两个召回相同的工具可能完全不同:一个说出了手法,
另一个只报告有东西被编码了。」

维度只从**触发了标记的** finding 取,与 aguard adapter 一致:从闸门下方的 finding 认领一个
类别,等于替它认领一份闸门从没给人看过的报告。

反向断言重跑仍然成立:aguard 的 `verdicts.jsonl` 与 2026-09-22 基线逐字节相同,
它的 attribution 也仍是 49/85 —— 语料 taxonomy 加一条没有动到任何既有的数。

**`corpus score` 的缺陷本身还在**:一个不报维度的工具仍然会拿到 `0 of N correct (0%)`,
和"每一类都说错了"无法区分。cc-audit 现在报维度了所以绕过了它,但下一个工具还会踩 ——
那条修法仍归语料仓,已单开。

## 实测结果(2026-09-23,尚未提交)

钉住 `v3.23.9`(sha256 核对),默认档,宿主机,3,539 个样本,3 分 22 秒。

**判据 1 成立**:`ledger: 3539 scored · 0 no-verdict · 0 error = 3539 of 3539`。
P-015 的账本不变量第一次在**不是我们写的工具**上被证实。

**判据 2 成立,并且以更强的形式**(2026-09-24 rebase 到新 `dev` 之后重做)。

原来的写法是"与 `2026-09-22` 基线逐字节相同",而上游那条 P-018(与本条无关的另一件事,
四种配置面形状)加了检测规则,aguard 因此从 85/300 变成 **87/300** —— 旧基线不再描述这棵树,
那条断言按字面已经失效,但**不是因为本分支**。

正确的形式不是"和一份旧基线比",是"**同一棵树上,我的改动没动 aguard 的答案**":
在 `origin/dev` 开一个 worktree 构建 aguard、跑同一份工作表,与本分支上的同一次运行
`cmp` —— **逐字节相同**。

顺带把过期的基线换掉:`baselines/results/aguard/2026-09-22/` 删除,
`2026-09-24/` 是当前树上的重跑。**两列因此都在语料 `5c3db6ee` 上**,
review 里那条"两列不是同一个语料提交测的"限度随之消失。

**「不做什么」第 5 条成立**:扫完之后 `git -C ../agent-artifact-corpus status --porcelain` 为空。

| 召回(恶意) | aguard | cc-audit |
|---|---|---|
| execution(fixture, n=52) | 18% [10,29] | **52%** [39,65] |
| exfiltration(n=48) | 53% [40,65] | **71%** [57,82] |
| injection(n=64) | 36% [26,48] | **55%** [43,66] |
| cisco-mcp-scanner-evals(n=127) | 11% [7,18] | **58%** [50,66] |
| skillcraft-audit(n=42) | 71% [56,83] | **81%** [67,90] |
| skillsgoat(n=65) | 26% [17,38] | **49%** [37,61] |
| 合计 flagged | 87/300 | **185/300** |

| 良性池 flag 率 | aguard | cc-audit |
|---|---|---|
| automatelab-mcp-tools(353) | **6%** [4,9] | 76% [71,80] |
| harvested(387) | **2%** [1,4] | 29% [25,34] |
| skillet-wild(474) | **5%** [3,7] | 43% [38,47] |
| skillmd-138k(1996) | **5%** [4,6] | 30% [28,32] |
| hard-negative(19) | **4 个** | 14 个 |

**它抓到的恶意样本是我们的 2.2 倍,同时在良性池上误报是我们的 6–12 倍。**
这是一笔清楚的召回/精度对调,不是单纯的输赢 —— 但无论怎么读,
**「cc-audit 覆盖窄」这句话已经死了**,它不能再出现在任何文档里(见「不能说什么」第 1 条)。

### 六个注入故障 fixture(第一轮全部记成 untestable,是我的缺口)

首轮六个全是 `untestable`,理由「adapter 不实现 FixtureRunner」。而未决问题 6 已决的是
**六个都跑** —— 那条出口是留给真的指不上去的工具的,而 `cc-audit check` 吃目录,
这六棵树它指得上去。所以那不是关于 cc-audit 的事实,是少实现了一个可选接口。补上后:

| fixture | 类 | 结果 |
|---|---|---|
| traverse-only-dir | 3 披露 | **pass**(带重要保留,见下) |
| symlink-cycle | 7 健壮 | pass |
| symlink-escape | 7 健壮 | pass |
| deeply-nested | 7 健壮 | pass |
| fifo-as-skill | 7 健壮 | **fail** —— 60 秒不返回 |
| sparse-huge-config | 7 健壮 | **fail** —— 限了界但放弃整棵树 |

判据全部取自语料自己的 `Expected` 字段(它写成工具中立的句子),**不套用 aguard 的机制**:
aguard 靠 `COV-000` 过 traverse-only-dir,cc-audit 根本没有 dimension-0 这个概念。

跑这六个又发现三件事:

1. **cc-audit 把 tracing 写进 STDOUT,挡在 JSON 前面**,`--format json` 在有警告时不可解析。
   3,539 个样本一次都没踩到 —— 这正是危险之处:它只在树里有读不了的东西时出现,
   而那恰恰是答案最要紧的树,并且会伪装成"output is not SARIF"。
2. **traverse-only-dir 它其实披露了**,写在 stdout,而且是日文:
   「ディレクトリエントリの読み取りに失敗。スキップします error=… Permission denied (os error 13)」。
   按语料的句子这是 pass。**但保留必须写在行上**:披露只在一行人读的日志里,
   结构化结果本身是 `passed: true`、零 finding —— 只读机器输出的消费者拿到的是一张
   干净的健康证明,而 `private/setup.sh` 里是一条真的 `curl | sh`。
3. **fifo-as-skill 真的挂住了。** 语料的原话:「A scanner that hangs here hangs on any
   repository containing a stray FIFO.」

`sparse-huge-config` 判 fail,但给它应得的一半:它确实在声明的 10 MB 限上把读取限了界、
报了文件名和两个数字,没去分配 8 GiB;没做到的是另一半 —— 语料要求超大配置
**本身就是那条 finding**,而它做成了致命错误、放弃了整棵树。


## Review(2026-09-23)

自查发现两处,都是本提案自己最该避免的那类错,已修:

1. **判据 3 点名了一个不存在的测试** `TestPickDispatchesToCCAudit` —— P-015 犯过、
   并被写进它历史当教训的那个错,在同一个位置重演了一次。而且不止是名字对不上:
   **没有任何测试断言 `pick()` 会为 `-tool ccaudit` 返回一个配好的 adapter**,
   已有的三个全是否定用例 —— 一个对什么都返回错误的 `pick()` 能把它们全部通过。
2. **`run.yaml` 把 `uploads_samples: false` 当事实印着,而它没有被证明。**
   保留意见写在 `tools.yaml`,而那不是引用数字的人会打开的文件。已加
   `uploads_samples_basis`,`Validate` 要求非空,三个工具都填。

### 仍然成立的限度,不打算修

- **对 cc-audit,没有任何东西能分开"读了没找到"和"根本没读"。** `ArtifactsNotEmitted`
  是被迫的,唯一兜底是 surface tripwire。本轮六个面全部有 flag,所以兜底没响 ——
  但那不等于它覆盖了每个面,只等于它在每个面上至少报过一次。
- **「两趟矛盾」那条路在 3,539 个样本上一次都没触发**,只有单测覆盖过。
- **维度映射测自 159 个样本**。`secretleak` 只有 1 次,等于没测;`supplychain` 赢
  `execution` 只赢 0.10。语料长大后要重测。
- ~~`corpus_commit` 两列不一致~~ **已解决**:aguard 基线 2026-09-24 重跑,两列都在 `5c3db6ee`。
- **`baselines/cmd/baseline` 覆盖率 4.1%**(本分支前 4.3%)。driver 基本没测,
  这是既有状况,`pick()` 只有拒绝路径 + 本轮补的正例。

## 完成

阶段 4 人说交付时本该填在 PR 里,我开 PR 时跳了这一步;PR #17 合入之后补上(2026-09-24)。

```
合入:PR #17(2026-09-24;sha 用 git log origin/dev --grep 'P-017' 找,24 个提交)
发布:v0.12.0
证据:TestPickDispatchesToCCAudit(baselines/cmd/baseline/pick_test.go);
     TestEmptySarifRunIsNotACleanBill、TestAToolThatEmitsNoArtifactsStillGetsAVerdict
     (baselines/adapter/sarif/fold_test.go);
     账本 3539 scored · 0 no-verdict · 0 error = 3539 of 3539(baselines/results/ccaudit/2026-09-23/ledger.jsonl);
     恶意抓到 aguard 87/300 → cc-audit 185/300,良性标记 147 → 1191,attribution 59% [48,68] → 39% [32,46]
     = 一笔召回/精度对调,"cc-audit 覆盖窄"作废,attribution 是唯一我们赢且区间不重叠的一轴;
     反向断言:origin/dev 上 worktree 构建的 aguard 与本分支同一工作表的 verdicts.jsonl cmp 逐字节相同
     (设计文档「判据 2 成立,并且以更强的形式」一节)
```
