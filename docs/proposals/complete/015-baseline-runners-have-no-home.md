<!-- SPDX-License-Identifier: MIT -->
# 015 — 对照表只能靠手写的规则数:第三方扫描器在同一份语料上跑不起来

- **来源**:新发现(2026-09-21,对话提出 —— "要在 Heeler 上测数据集,后面还要更多第三方扫描器,需要一个合适的地方放它们")
- **依赖**:P-011(verdicts schema 与 runner 形状)
- **分支**:`p/015-baseline-runners-have-no-home`
- **编号沿革**:**开号时取 013**(当时 `origin/dev` 索引最大号是 012);实现期间上游合入了
  `013-config-url-literal` 与 `014-release-0.11.0`,**撞号,按守则"后推的改号"改为 015**。
  提交信息里的 `(P-015)` 已全部重写为 `(P-015)`,否则 `git log --grep` 会把两份 013 混在一起 ——
  README 的回溯承诺正是靠那个 grep。先例:P-006(原 005,撞号改号)。

## 问题

**首轮基线只有一列数字,而且结构上不可能有第二列。** `make bench` 给出的是 aguard 自己的成绩
(plan §3.1:能看到的 173 个恶意样本拦下 71 = 41%;3,220 个真实良性误拦 219 = 6.8%;
hard negative 4/19)。**同一份样本上没有任何第二个扫描器的数字**,所以 41% 是高还是低、
6.8% 是可接受还是灾难,今天答不出来 —— 一个没有对照物的率,只能和自己的上一次比。

而计划已经排了这张对照表:`plan §8` 交付物清单里 `docs/comparison.md` 归 **M2.3**,
"生成方式"一栏写着**手写**;M2.3 的 E.9 目前唯一具体的对照项是"规则数写 72 vs 72 并说明口径"。
这正是 `direction §4` 判过死刑的那个轴 —— **"规则数竞赛:彻底放弃……它既不说明检出率也不说明
误报率"**,同一节还写明"**真正的答案是语料上的精确率/召回率**"。所以现状是:自己承认唯一有意义的
对比轴是语料,却没有能力在语料上做对比,于是计划里留着一张用自己否决过的轴手写的表。

**结构性的原因是 runner 无家可归**,而这不是疏忽,是现有设计对"我们测别人"这个方向直接失效:

1. **放不进语料仓。** `docs/using-the-corpus.zh-CN.md` 的协议写明第 2 步 runner 是"唯一需要你写的",
   按设计住在**扫描器自己的仓库**里;`make validate` 的中立性检查更进一步,**禁止 harness 代码里
   出现任何注册扫描器的 id**(README 记着这条检查抓到过一个把字面量 `"aguard"` 钉进去的 CI 步骤)。
   一个 `heeler` runner 放进 `harness/` 会被自己的闸门拦下,而且拦得对。
2. **放不进本仓库。** `hack/corpus-runner`(635 行)是 aguard 专用的。把竞品的 runner 摆进
   **被测工具自己的仓库**,等于由参赛的一方写对手的答题卡。语料仓的文档已经写明这条路会怎么错:
   "**某个面上突然出现一整列 0,几乎总是这个原因(摆放),而不是真漏。**" 一个由我们写的、把样本
   摆错了位置的 heeler runner,产出的是一个**对竞品不利的假数字,并且署我们的名发出去**。
3. **跑出来的东西也没地方放。** verdicts、原始报告、以及"哪个版本/哪个阈值/哪天跑的"这套元数据,
   目前没有任何目录承认它们。M1.2 已经为 skillsgoat 定过这四项的形状(最后运行日期、扫描器版本、
   快照、`blind: true`),但那是我们**被**别人测,反过来没有对应物。

**查证中确认的四件事,每一件都改变"合适的地方"是什么形状:**

- **Heeler 不是纯本地工具。** 它确实有单二进制 CLI(`heelercli`,`scan-agent-file`,SARIF,
  exit 1/0),但官方页面写着**多数功能需要 heelercli 已安装并认证**,检测**与平台同源**,
  只有 secret scanning 完全不需要账号。所以跑它至少要一个账号,而且很可能样本正文会离开本机。
- ~~**许可:这一条比 ToS 更硬,而且可计算。**~~ **这条整段是我的错,已撤回,见下面的更正二。**
  语料仓 `docs/licensing.md` 开篇把两个问题分开,并说"**混淆这两个是常见错误**":
  (1) 可否**再分发** —— 管 vendored 的 **layer 1**(`corpus/`);(2) 可否**测量并发布汇总数字**
  —— 管 **layer 2**(`manifest/`,只有 URL 和哈希)。**我把 layer 2 的标记当成了 layer 1 的内容,
  犯的正是那句话点名的错误。** 上传范围不是一个有名单的许可问题;剩下的只有 ToS(未决 #2b)。
- **污染:公开的本地扫描器里,有两个的 fixture 已经在我们语料里。** 现有来源含
  `cisco-mcp-scanner-evals`(**127 个样本**)、`nvidia-skillspector-fixtures`、`skillsgoat`;
  而能本地跑的开源扫描器恰好就是 Cisco skill-scanner、NVIDIA SkillSpector 这几个。
  `manifest/corpora.yaml:35` 已经为同一种形状拒过一次双重计数。**而 `corpus samples` 今天
  没有任何 flag** —— 连"去掉它自己的来源再算一遍"都做不到。好消息是数据已经在:
  `corpus score` 已经按来源分组,缺的只是一个入口。
  **(实现时更正)**:入口要复用的是 `harness/cmd/corpus/basis.go` 的 `populationOf`,**不是**
  `source.go` 的 `originOf`。`score.Score` 的注释明写着 population 是被注入的,为的是
  "one definition of 'source', checked in one place";`originOf` 服务的是集中度报告(按原始仓库),
  拿它来过滤会造出第二个"来源"定义,而它的数字与成绩单无法对照。
- **安全:有的扫描器会执行被扫内容。** Snyk `agent-scan` 的说明称扫 MCP 配置时会执行其中定义的
  命令(**待复核,只是读到的说法**)。指向 300 个恶意样本就是在本机执行恶意载荷。本仓库
  不变量 #1"绝不执行被扫内容"只管 aguard 自己;跑别人的工具时,它提供的保护是零。
  所以"合适的地方"必须包含**隔离**,而不只是一个目录。Heeler 不在这个名单上,所以这条不挡第一轮。

> **更正一(2026-09-21,draft 阶段写错)**:draft 里说那 387 个 `harvested` 良性样本"从本机采集",
> **这是错的**。逐个读 `origin.source` 确认:387 个全部是**公开 GitHub 仓库里带 commit 固定的
> `.claude/settings.json`**(如 `github.com/ayutaz/piper-plus/blob/244ffeb…/.claude/settings.json`)。
>
> **更正二(2026-09-21,design 阶段写错,而且是这份文档里最严重的一处)**:上面那条"许可名单"
> **不存在**。三条实测:
>
> 1. **`corpus samples` 输出的 3539 条,`path` 全部在 `corpus/` 下,无一条指向 `cache/`。**
>    也就是说测试点全在 layer 1。
> 2. **layer 1 的每个标签都被强制为宽松许可。** `harness/internal/label/label.go:335` 在
>    `make validate` 里拒绝任何不在 `permissiveLicenses`(MIT · Apache-2.0 · BSD-3 · BSD-2 ·
>    CC-BY-4.0 · CC0-1.0 · ISC)里的 `origin.license`,报错原文是"may not be vendored into
>    layer 1 — reference it from manifest/ instead"。
> 3. **那 3 个我当成"不可分发"的 mcptox 样本,是我们自己的 MIT 重写件**:
>    `type: reconstruction`、`license: MIT`、source 指向论文而非上游仓库。
>    `vendorable: false` 描述的是**上游语料能不能被 vendored**,不是树里任何东西的性质。
>
> **而且语料仓本身已经是公开的**(`gh repo view basdotio/agent-artifact-corpus` → `PUBLIC`),
> 所以交给 Heeler 的任何样本它本来就能 `git clone`。
>
> **后果**:未决 #3 作废,工作项里的"上传闸门"作废(它防的东西不存在)。而这是本轮**唯一一条
> 后果不可撤回的未决问题** —— 它消失了,不是被答了。剩下的上传相关顾虑只有 ToS 一条。

## 初步方向

**本仓库根下一个 `baselines/` 目录**(领导 2026-09-21 决定,见「已决」)。一个扫描器一个 adapter,
输出就是语料协议的 `verdicts.jsonl`,打分继续用 `corpus score`,外加一份**覆盖账本**和一份运行元数据。
`aguard` 自己的结果也进同一棵树,于是"对照"就是同一个目录下两个文件的 diff。

```
baselines/
  README.md                      # 读法、署名声明、"暂定值"声明
  tools.yaml                     # 每个工具:版本、阈值档、是否上传、是否执行被扫内容、逐面声明
  adapters/<tool>/               # 唯一了解那个工具的代码
  results/<tool>/<YYYY-MM-DD>/
    verdicts.jsonl               # 提交(实测 306 KB)
    ledger.jsonl                 # 提交 —— 每个样本一行,scored / no-verdict / error(决定 2)
    scorecard.txt                # 提交 —— `corpus score` 的原样输出,不转述
    run.yaml                     # 提交 —— 元数据,含语料 commit sha
    raw/                         # 不提交(实测 14 MB/次/阈值,可再生,且含第三方报告原文)
```

**初稿提议新建一个独立仓库,被否决了。** 否决的代价是清楚的:adapter 和结果现在住在**被测工具
自己的仓库**里,"参赛者写对手答题卡"这个指控无法用仓库位置来回答了。所以那条纪律从文档句子
升级成**输出里的字段** —— `run.yaml` 必须记下是谁的 adapter、谁选的阈值、谁做的摆放,
成绩单每次都带着它印出来。位置挡不住的,只能靠可复算和可反驳挡。

**决定 1 顺带解掉了未决 #8**:十条有效工作项里八条回到本仓库,只剩两条在语料仓 —— 正好落回
P-005 的先例范围,`docs/process.md` 不需要补跨仓规定。

**一个不在计划内的收益**:`bin/bench/` 是 gitignore 的,所以 P-011 的基线今天只以散文形式存在于
`plan §3.1` 里,"aguard 的数字一行都没动"这条反向断言**根本无从机械验证**。把 aguard 自己的
verdicts 提交进 `baselines/results/aguard/`,这条断言才第一次真的可以跑。

### 覆盖账本:每个测试点都有下场,不能测的单独标出来

领导 2026-09-21 定的第二条:**语料里所有测试点都要测,测不了的单独标记。** 这正是不变量 #5
("任何遗漏都不许静默")作用在测量装置自己身上,所以做法照它:每个样本落进**恰好一个**桶,
桶里写原因,而不是从输出里消失。

**范围就是这份语料,一个点不多一个点不少**(领导 2026-09-21 再确认)。完整的测试点集合是
**3539 + 6**,两条命令穷尽它,没有第三处:

```bash
go run ./cmd/corpus samples | wc -l      # 3539,实测全部 path 都在 corpus/ 下,无一条指向 cache/
go run ./cmd/corpus fixtures             # 6 个注入故障,不在 samples 里
```

**不从别处引入测试点**(自造用例、别的 benchmark、真机扫描),也不给这份语料**加**样本 ——
加样本是 `plan §3.1` 第 7 步在 corpus 仓做的事。

**每一个点都要被扫描器真的跑过。** 所以下场只有三种,而且判定"跑不出来"必须**发生在调用之后**:

| 下场 | 含义 |
|---|---|
| `scored` | 出了判定,进 `corpus score` |
| `no-verdict` | 扫描器**被指向了这个样本**,但没产出可折成一个词的东西;带该工具自己的理由 |
| `error` | 超时、崩溃、输出解不开 |

`skipped` 这个下场**不存在**:账本里任何一行都不允许是"我们没跑"。

**这一条要改 `hack/corpus-runner` 的现有行为。** 今天 `run()` 在 `stage()` 返回 `notPlaceable` 时
**直接 return,`runAguard` 一次都没被调用**(`hack/corpus-runner/main.go:526`)—— 那 127 个样本
从来没有被交给扫描器。更要紧的是它把"aguard 读不到"写进了**共享的那一层**:
`no-load-path` 是**每个工具各自的**属性,不是语料的属性。NVIDIA SkillSpector 接受"目录和单个文件",
所以那 127 个 `.py` 对它很可能是**可测的**。一个工具的产品边界不该决定另一个工具被测到多少。

**但"跑过了、什么都没报"绝不等于 benign。** 那会把一条已披露的产品边界记成一次正确的良性判定,
正是现有 runner 的注释拒绝的事("Calling it benign would be a runner scoring itself")。

> **更正三(2026-09-21,W5 实现时实测,推翻本节的预期)**:本节原来写"那 127 行是 `no-verdict`"。
> **不对。** 把 `aguard check` 指向裸树,它**读得到 `.py` 的内容**:127 个里 55 个至少一条发现,
> **14 个在 `high` 触发闸门**。所以它们是真实的命中与真实的漏检,不是覆盖缺口 ——
> 127 行全部 `scored`,带新增的 `secondary-surface` 标记。
> **后果是一个已发布数字变了**:恶意召回从 71/173 = 41%(127 个不进分母)变成 85/300 = 28%。
> 41% 不是算错,是一个讨好自己的分母。误报 5.5% 与难负例 4/19 不变(那些样本一直是可摆放的)。
> `no-verdict` 因此只剩一个可达条件:`check` 连一个 artifact 都没产出。

**三个标记从"下场"降级为"scored 行上的标签"**,因为它们都不是测不了:
`own-fixture`(样本出自这个工具自己的测试集)照样测,只是发布时给两个分母;
`surface-undeclared`(这个面还没声明覆不覆盖)照样测,那句声明只管**怎么读**一个整面全零,不管是否测;
`secondary-surface`(更正三新增,判定来自另一个入口)同样照样测,欠一个分母说明。

**账本必须穷尽,并且有测试钉住**:`scored + no-verdict + error == 3539`,逐个样本对得上,
且 `skipped == 0`。今天的 runner 对这些样本**不输出任何行**,靠 `corpus score` 事后数出缺口 ——
那是"事后发现少了",不是"当场说明为什么少"。差别在一次改动之后才显现:沉默的缺口不会告诉你它变大了。

**6 个注入故障 fixture 也是测试点,而它们一次都没被跑过。** `corpus fixtures` 的六个
(0111 不可枚举目录、顶替 `SKILL.md` 的 FIFO、符号链接环、逃逸到 `/etc/passwd` 的链接、
4096 层深链、8 GiB 稀疏 `.mcp.json`)**不在 `corpus samples` 里**,`make bench` 三步一步都不碰它们
—— 对 aguard 自己也从未跑过。它们是**逐个通过/失败,不是率**(单个反例即定论),由 runner 判、
不由 `corpus score` 判。其中 `traverse-only-dir` 是**披露**测试,正对着不变量 #5:
对一个列不出来的子目录什么都不说,等于把没读完的 skill 当成读过了。
`--restore` 不是可选的 —— 那个 0111 目录会挡住 `rm -rf` 需要的枚举。

### 我们自己也在污染名单上(实测,2026-09-21)

一直在说 Cisco 的 127 个 fixture,同一个问题我们自己也有。语料里有 **3 个样本的 `origin.source`
指向 agent-guard 自己的工作项**:

| 样本 | 类别 | 来源 |
|---|---|---|
| `hard-negative/instruction/defensive-refusal-prose` | 难负例 | W-009 |
| `hard-negative/skills/environ-copy-subprocess` | 难负例 | W-008 |
| `malicious/skills/revshell-python-dup2` | 恶意 | W-027 |

**难负例普查一共只有 19 个,其中 2 个是我们自己写的。** 而 `plan §3.1` 五道门里有一条是
"hard negative 误拦 ≤ 1/19" —— 那个分母里 **10.5% 是我们自己的 fixture**,而这五道门是拿来
判断"高"到没到的。这不是本 proposal 要修的东西,但账本必须把它们标成 `own-fixture`,
并且**发布 aguard 的难负例数字时给 19 和 17 两个分母** —— 和我们要求 Cisco 的那条规则完全一样。
对自己不执行的规则不是规则。

### 否决过的备选:让他们自己测

**最干净的方案不是我们代跑,而是公开语料、请对方自己跑、引用他们发布的数字。**
语料仓的协议本来就是为这件事写的 —— `docs/using-the-corpus.zh-CN.md` 第一句就是"你不需要向我们要
任何东西:不用账号、不用注册、不用在本仓库里登记任何条目"。这条路径**在结构上避开了本 proposal
最大的风险**:摆放是我们做的选择,而摆错了就会产出一个对竞品不利的假数字。对方自己跑,这个风险归零。

**否决的理由只有一条,而且是时间不是道理**:阶段 1 的门槛是"能说出一句带出处、带 n、可被第三方
复算的成绩",而一个没有对照物的 41% 说不出这句话;把语料公开并等别人来跑,可能永远不发生,
而 M2.3 已经在排期上了。

**所以这不是一个更好的方案被否决,是一个更好的方案太慢。** 两个后果写进纪律:
(1) 我们代跑的数字在对方自己跑过之前都是**暂定值**,发布时必须这么说(见「不能说什么」);
(2) adapter 与结果应当**可被对方拿去复算或反驳**。决定 1 之后这一条的分量更重:结果住在被测
工具自己的仓库里,位置不再提供任何中立性,所以**可反驳性是唯一剩下的辩护**。
本仓库在 M2.2 会公开(`plan §4`),届时 `baselines/` 随之公开 —— 这不是附带效果,是这条纪律的兑现方式。

## 完成的判据

- [x] **账本穷尽,由 `TestCheckRejects`(14 条拒绝用例)、`TestCheckAcceptsAnExhaustiveLedger`、`TestTallyAddsUp` 钉住**(`baselines/ledger`;判据初稿点名的 `TestLedgerIsExhaustive` 这个名字不存在,已改为实际名字 —— 一条引用不存在测试的判据无法验证):对任意一次运行,
      `scored + no-verdict + error` **逐个样本**等于 `corpus samples` 的行数(实测 **3539**),
      一个都不许凭空消失;**`skipped == 0`**;每条 `no-verdict` 都带一个来自封闭取值集的原因码,
      **且该行 `attempted` 为真** —— 没被调用过就不允许写理由。
      **这条是决定 2 的执行装置**,也是本 proposal 唯一一条"没有它其余判据都可以靠沉默通过"的判据。
- [x] **反向断言(把 127 个样本接进账本没有改动任何已有判定)**:改完 `hack/corpus-runner` 之后,
      3412 个原本可摆放样本的 verdicts 与改动前**逐字节相同**(`diff` 空输出);
      账本只**新增** 127 行 `no-verdict`,不改任何一行既有结论。这是这次唯一真正危险的改动 ——
      为了让 127 个点被跑到而动了摆放逻辑,顺手改掉别的 3412 个的结果。
- [x] **6 个注入故障 fixture 每个都有结论**,逐个 `pass` / `fail` / `untestable+原因`,**绝不折成率**;
      跑完必定执行 `corpus fixtures --restore`(否则 0111 目录留在盘上,`rm -rf` 删不掉)。
      **(实现时更正)**:判据原文写"在 `ledger.jsonl` 里六行"—— **不行**。fixture 不在
      `corpus samples` 里,所以 ledger 行会被 `ledger.Check` 以"不在工作清单里"拒绝,而为了塞进去
      放宽那条检查,等于钝掉整个目录赖以成立的那一个不变量。改为**独立的 `fixtures.jsonl`**。
      **首次运行结果(2026-09-21,历史上第一次)**:六个全 `pass`;`traverse-only-dir` 是靠
      **`COV-000`** 过的 —— 不变量 #5 真的在工作,而这是第一次被验证。
      **一处诚实的弱化**:`sparse-huge-config` 断言的是**有界读取**,而这个 runner 只能从进程外
      观察到"终止了",量不到峰值内存。这句话写在那一行的 `detail` 里,跟着结果进
      `baselines/results/`,而不是只留在包注释里。
- [ ] **未做(W6/W11,不在本次 PR 范围)**:Heeler 的第一组数字可被 `corpus score` 读:`corpus score <heeler 的 verdicts>` exit 0、
      **解析错误 0 行**,头部写明阈值与未覆盖数,至少一个来源给出带 n 的率。
- [x] **摆放自查,由 `TestFiresWhenNothingIsFlaggedOnALargeSurface`、`TestADeclarationTurnsAFailureIntoADisclosure`、`TestADeclarationForASurfaceWithFindingsIsAMistake`、`TestTheCrossoverIsTwentyTwo` 钉住**(`baselines/tripwire`;同上,初稿的 `TestSurfaceNeedsADeclaration` 不存在):当某个面上**达到 `truth.severity` 的恶意
      样本**一个都没被报时,命令 **exit 非 0 并要求一次人工声明**,不自动过也不自动失败。
      谓词只看恶意样本 —— 良性池上全清正是正确答案,拿"全是良性"当触发条件会在良性池上一直响。
      而反应必须是"要一句声明"而不是"失败",因为全清有两种原因、机器分不开:**我们的摆放错了**
      (语料文档点名的失败模式),还是**这个工具本来就不覆盖这个面**(Heeler 很可能不读
      `settings.json` 的 hooks)。后者是一条该被声明的覆盖缺口(不变量 #5 的形状),把它记成失败
      是在惩罚一个已披露的产品边界。声明按"工具 × 面"存一次,进 W3 的元数据。
      **只在 n 够大的面上有意义**:实测恶意样本分布是 `skills 153 · mcp 130 · permission 6 ·
      hooks 5 · connector 5 · instruction 1`,后四个面"0 命中"毫无统计异常(语料自己就说这些面
      报覆盖度不报率),而 mcp 面见下面那一节已被打空 —— 所以这条自查实际只作用于 skills。
- [ ] ~~`TestUploadGateRefusesNonVendorable`~~ **作废**(更正二):它防的那批不可分发样本不在树里。
      语料仓自己的 `make validate` 已经是这道闸门,再加一道是重复实现一个别处已经成立的不变量。
- [x] **反向断言(语料仓的中立性没被放宽)**:语料仓 `make validate` 仍 exit 0,且
      `git grep -il heeler harness/` **0 命中**。
- [x] **反向断言(aguard 自己的数字一行都没动)**:`make bench` 产出的
      `bin/bench/high/verdicts.jsonl` 与**提交进 `baselines/results/aguard/` 的基线** `diff` 空输出。
      注意这条判据本身是这次才**变得可验证的** —— `bin/bench/` 是 gitignore 的,P-011 的基线此前
      只以散文形式存在于 `plan §3.1`,拿它做反向断言是空头承诺。
- [~] **部分达成 —— 自己的污染也给两个分母**:已写进 `plan §3.1` 与 `baselines/README.md`,但**没有任何东西机械地强制它**,靠的是纪律。要做成输出里的字段,得等有第二个工具来对照(W6 之后)。原文:aguard 的难负例数字同时给 **19**(全部)和 **17**(去掉 W-008、
      W-009 两个我们自己的 fixture)。对自己不执行的披露规则,拿去要求 Cisco 时不成立。
- [x] **污染可算,且排除的代价被打印出来**:`corpus samples --exclude-source cisco-mcp-scanner-evals | wc -l`
      比不带 flag **少 127**,**并且**成绩单在排除后写明"恶意 MCP 面剩 3 个样本,不构成率"
      (见下面「一个面对比不了」)。只给前半句的判据会把一次删面读成一次去污染。
- [~] **部分达成 —— 分母差异由机器打印**:语料仓一侧做了(`corpus score` 现在按来源分组打印未覆盖样本,并标出"整个来源");"与 aguard 基线的差集"这半截**没做**,因为今天没有第二个工具的基线可差(W6 之后)。原文:成绩单必须自己输出"本次分母与 aguard 基线的
      差集及原因"(哪些来源、各多少个、为什么)。这是 `using-the-corpus` 引用规则第 2 条
      ("任何排除都必须出现在结论行里,并给出两个数字")的机器化版本 —— 写在纪律里的规则会被忘,
      写在输出里的不会。
- [~] `make verify`:六个闸门里四个过,两个未过且原因都不是本次改动 —— 见「完成」。原文:**这一轮本仓库有真代码了**(`baselines/`、改动过的 `hack/corpus-runner`),
      所以这条不再是走过场:`go vet`、`-race`、lint、`rules.md` 无漂移、plugin 自扫全部要过。

### 一个面对比不了,这是实测结论不是选择

`corpus/malicious/mcp` 共 **130 个样本 = 127 个 cisco 派生 + 3 个 `mcptox-*`**(2026-09-21 实测)。
三件事同时成立:

- 那 127 个 cisco 样本**就是** bench 里未覆盖的那 127 个(`server source (.py) has no load path
  aguard reads`)—— 同一批,不是两件事。
- 给 Cisco 的分数去污染 = 排除这 127 个 = 恶意 MCP 面**只剩 3 个**。
- 那 3 个是我们自己的 MIT 重写件(更正二),所以**任何工具都能测它们** —— 但 3 个不构成率,
  语料自己的规矩是这种规模只报计数。

所以 MCP 面上**没有一个干净的对比分母**:含那 127 个,给 Cisco 打的分是打在它自己的 fixture 上;
去掉它们,剩 3 个,不构成率。`manifest/corpora.yaml:2635` 已经为相邻问题写过同一句话
("同一序列化下的恶意样本,本语料一个都没有")。**能给出率的对比只在 skills 面**(153 个恶意样本);
其余面按语料自己的规矩报计数、不报率。补这个面属于 `plan §3.1` 第 7 步(语料扩充,在 corpus 仓做),
**不在本 proposal 范围内**。

注意这不与"每一个点都要测"冲突:那 127 个点**照样要被每个工具跑过并记进账本**(对 SkillSpector
之类接受单个文件的工具,它们很可能是可打分的)。**"测了"和"能拿它算一个率"是两件事** ——
账本管前者,`corpus score` 管后者。

## 不做什么

- **不改 aguard 的任何检测规则。** 证明:`git diff --stat origin/dev -- internal/` 为空。
  ~~不改 `hack/corpus-runner`~~ —— **这条边界作废**:"每一个点都要测"要求它停止在调用扫描器之前
  就把样本过滤掉(`main.go:526`),所以它必须改。改动被上面那条反向断言夹住:3412 个既有判定
  逐字节不变,只新增 127 行。**把一条作废的边界留在这里而不是删掉**,因为"这条边界曾经存在、
  为什么不再成立"本身就是要审的东西。
- **不引入这份语料之外的任何测试点。** 不自造用例、不接别的 benchmark、不把真机扫描算进账本。
  证明:账本里每一行的 `sample` 都能在 `corpus samples` 的输出里找到,行数相等。
- **不把任何第三方工具名写进语料仓 `harness/` 或任何 `truth` 块。** 证明:第 4 条反向断言。
- **不给第三方工具写 `expect.<tool>` 块。** 那会让语料去描述竞品的模型 —— 正是中立性检查
  第一条禁止的事。注册进 `taxonomy/tools.yaml` 与写 `expect` 块是两件事,只做前者。
- **不在任何 CI 里跑第三方扫描器。** 和 `make bench` 同一条纪律:维护者机器上按需跑,
  永不成为 CI 依赖。
- **不 vendored 任何第三方扫描器的二进制或规则库**进任何仓库。
- **不对外发布对照数字。** 这一轮只产出数字与读法;发布是 M2.3,且要人拍板。
- **不改 `docs/comparison.md` 本体**,只改 `plan §8` 那一行的"生成方式"。
- **不为了迁就某个第三方工具去改语料的任何样本或标签。**
- **不改 `corpus score` 的任何算术。** W7 只加披露行(排除了什么、代价多少);率怎么算、区间怎么定、
  census 与 estimate 怎么分开,一个字节都不动。证明:`git diff origin/main -- harness/internal/score/` 为空。
- **第三方的原始报告(Heeler 的 SARIF 等)不进语料仓的任何位置。** 只落在 `baselines/`(且不提交)。否则竞品的规则名和
  分类就进了语料中立的那一半 —— 中立性检查第一条禁的正是这个,而它只扫 `harness/` 与 `truth`,
  扫不到一个被塞进 `docs/` 或 `cache/` 的报告文件。**这条得靠边界,拦不住它的闸门不存在。**
- **不补 MCP 面的恶意样本。** 那是 `plan §3.1` 第 7 步的事,在 corpus 仓做。本轮只**声明**这个面
  对比不了(见判据那一节)。
- **不提交 `raw/`。** 实测 14 MB/次/阈值,可再生(记 `run.yaml` 里的语料 commit sha 即可重跑),
  而且第三方报告原文里带恶意样本的片段。提交的只有 verdicts、ledger、scorecard、run.yaml。
  也不提交 `samples.jsonl`(622 KB,由语料在固定 commit 上再生)。
- **不是每次跑都往 `baselines/results/` 里提交。** 只提交**被引用的**那些运行:一次基线、
  一对前后对比。否则这个目录会变成实验垃圾场,而"哪个是基线"就又没有答案了。
- **不改 `plan §3.1` 的五道门。** 发现了难负例分母里有 10.5% 是自己的 fixture,本轮只**披露**,
  不动门槛数字 —— 改门槛是拿主意的事,不是测量装置的事。

## 不能说什么

- **不说"我们比 Heeler 准/强"。** 只能说"在这份公开语料的这些来源上、阈值 T 下,
  aguard A%、Heeler B%,各带 n"。
- **任何第三方的数字必须署我们的名。** 是我们写的 adapter、我们选的阈值、我们做的摆放 ——
  少这句署名,它就是一个我们无权下的结论。
- **Cisco / NVIDIA 的数字不得省略污染披露**:含/不含它们自己 fixture 的**两个数**都要给
  (`using-the-corpus` 的引用规则第 2 条)。
- **不说"首个/唯一/第一个把竞品放在同一语料上测的"** —— `direction §4` 的三类词禁令。
- **不把"需要账号/与平台同源"说成安全缺陷。** 那是产品形态差异,不是安全结论。
- **不用"语料里没有这种东西"当论据** —— `using-the-corpus` 的明令。
- **没有 `dimension_map` 的工具,归类分报"该工具不可测",不报 0。** 这是不变量 #5 的形状:
  缺口要声明,不能算成失分。
- **在对方自己跑过之前,我们给出的任何第三方数字都只是暂定值,而且必须这么说。** 见下面
  「否决过的备选」:语料本来就是给第三方自测用的,我们代跑只是因为等不到,不是因为代跑更准。
- **不说 MCP 面的任何对比结论。** 那个面对比不了(判据那一节),而"没有数字"不等于"表现相同"。

## 工作项

| W | 一句话 | 提交信息(不写 sha) |
|---|---|---|
| 1 | **测试先红**:账本穷尽 —— `scored + no-verdict + error` 逐个样本等于 3539,`skipped == 0`,原因码取值封闭 | `baselines: a sample that produced nothing must say why, not vanish from the ledger (P-015)` |
| 2 | **测试先红**:某个面上达标的恶意样本一个都没报 → 要一次人工声明,不自动过也不自动失败 | `baselines: a surface with nothing flagged needs a declaration, not a silent pass (P-015)` |
| 3 | `baselines/` 骨架:目录布局、`tools.yaml`、语料协议读写、四字段输出、空 adapter 接口 | `baselines: where a scanner's score and the reason it has none both live (P-015)` |
| ~~4~~ | ~~上传闸门~~ **作废(更正二)**:它防的不可分发样本不在树里,语料仓 `make validate` 已是那道闸门 | — |
| 5 | `aguard` adapter:接进账本,**让那 127 个样本真的被调用一次**(今天 `main.go:526` 直接 return),**并补上从未跑过的 6 个 fixture** | `baselines: aguard's own six fixtures had never run, and 127 samples were never handed to it (P-015)` |
| 6 | `heeler` adapter(`heelercli scan-agent-file` → SARIF → 四字段 verdict) | `baselines: heeler adapter — SARIF folded to the one word the corpus scores (P-015)` |
| 7 | 元数据与署名:工具版本、policy 哈希、阈值、是否上传、语料 sha、逐面声明、**谁的 adapter** | `baselines: a number nobody can date, version or attribute is not a measurement (P-015)` |
| 8 | 隔离:标着"会执行被扫内容"的工具,无容器时拒绝跑(不降级) | `baselines: a scanner that executes what it scans does not run on the host (P-015)` |
| 9 | 语料仓 `corpus samples --source/--exclude-source`(复用 `populationOf`,**不是** `source.go`,见下)+ 排除的代价写进输出 | `corpus: a contamination-free denominator needs a flag, not a footnote (P-015)` |
| 10 | 语料仓 `docs/licensing.md` 补一条:"测量不是分发"建立在**本地运行**上,非本地工具下不成立 | `corpus: "measuring is not distributing" assumed the tool runs locally (P-015)` |
| 11 | 跑 Heeler,出第一组数字与读法;`plan §8` 那行从"手写"改为由 `baselines/` 生成 | `plan: docs/comparison.md comes from a measurement, not from rule counts (P-015)` |

**W1–W3、W5–W8、W11 在本仓库,只有 W9–W10 在语料仓(W4 已作废)。** 决定 1 把比例翻了过来,未决 #8 因此作废。
W1 和 W2 都是测试先红:W1 管"有没有东西悄悄消失",W2 管"消失的那批是不是我们摆错了"。
**W4 作废之后,排序约束也随之消失** —— 它是初稿里唯一一条"不可撤回、必须排在前面"的工作项。
留下的最危险一步变成 **W5**:为了让那 127 个点被跑到而动摆放逻辑,顺手改坏另外 3412 个的结果。
夹住它的是判据里那条"3412 个既有 verdicts 逐字节不变"。

### 对排期的实质影响(依赖行没写全)

W11 把 `docs/comparison.md` 从"手写"改成"由测量生成",于是 **M2.3 多了一个前置**:它原来只依赖
M1.6(要有自己的数字),现在还要等第三方的数字。**这会推迟 M2.3**,而 M2.5(清单收录)依赖 M2.3。
这不是本 proposal 附带的小事,是它花掉的成本之一,写在这里而不是留给人自己发现。

## 未决问题

> **已决(2026-09-21,领导):剩下的全部按建议答案。** 逐条落成:
> **#1** 目录 `baselines/`(不叫 `bench/`);提交 verdicts + ledger + scorecard + run.yaml,
> 不提交 `raw/` 与 `samples.jsonl`;只提交被引用的运行。
> **#2** a 工作邮箱单独注册、不接任何真实仓库;b 跑之前读一遍 `/terms`,有禁止评测条款则只内部用;
> c 确证不了离线就按"会上传"处理。
> **#4** `corpus samples` 默认**不**排除,排除必须显式;`corpus score` 头部固定打印本次含/不含哪些来源。
> **#5** 取 Heeler 最接近"让构建失败"的那一档(**不是**默认 policy),记 policy 哈希,成绩单写明是我们选的;拿不准跑两遍。
> **#6** 进 `taxonomy/tools.yaml` 只为归类分;`dimension_map` 必须来自它自己公布的分类,拿不到就不写、归类报"不可测";不写任何 `expect.heeler` 块。
> **#7** 抵扣 `docs/comparison.md` 的手写对照表;明确"按需跑、永不进 CI";§10 点名的两个待减项本轮不动。
> **#9** Docker(`--network none`、样本只读挂载、tmpfs 写层);无 Docker 时对"会执行被扫内容"的工具**拒绝跑**,不降级。
>
> **全部已决 = 设计被接受(`docs/process.md` §1 阶段 2 准出)。** 进入阶段 3。
>
> **已决(2026-09-21,领导,第三次确认)**:(3) **范围就是 `agent-artifact-corpus` 的数据点,
> 一个不多;而每一个都要测。** → 测试点集合固定为 **3539 + 6**,两条命令穷尽;`skipped` 下场不存在;
> "跑不出来"的判定必须发生在**调用扫描器之后**,而不是之前(这一条要改 `hack/corpus-runner`);
> 两个污染标记从"下场"降级成"scored 行上的标签"。
>
> **已决(2026-09-21,领导)**:(1) **不新建仓库**,在 agent-guard 下单开一个目录放所有测试结果
> → 原未决 #1(新仓名字与公开性)作废,改为下面的 #1(目录布局与提交范围);原未决 #8
> (跨仓记账)**作废**,九条里七条回到本仓库。(2) **语料里所有测试点都要测,测不了的单独标记**
> → 成为「覆盖账本」那一节与 W1,并捞出 6 个从未跑过的注入故障 fixture。

1. **目录布局与提交范围。** 建议 `baselines/`(根下,不与 `bin/bench/` 冲突,实测无同名目录),
   提交 verdicts + ledger + scorecard + run.yaml,**不提交** `raw/`(14 MB/次)与 `samples.jsonl`
   (622 KB,可再生)。另一个问法是要不要干脆叫 `bench/` —— 我建议不叫,`make bench` 已经占了
   这个词指"跑一次",目录叫同名会让"bench 的输出在 bin/bench 还是 bench"变成一个每次都要想的问题。
2. **Heeler 的三个接入前提。**
   a. 账号用谁的?建议工作邮箱单独注册,**不接入任何真实仓库**。
   b. ToS 里有没有禁止发布评测的条款?建议**跑之前**有人读一遍 `/terms`,结论抄进本节"已决";
      读不到或有禁止条款 → 数字只内部用,不发布。
   c. `heelercli` 有没有真正的离线模式?建议:能确证不上传就按本地待遇,**确证不了就按"会上传"处理**
      (保守侧,因为第 3 条的后果不可撤回)。
3. ~~**许可:9 个 `vendorable: false` 的来源要不要交给 Heeler?**~~ **已决(2026-09-21):问题不存在,作废。**
   见更正二:3539 条测试点全在 layer 1,每条的 `origin.license` 都被 `make validate` 限定在宽松许可集,
   那 3 个 mcptox 是我们自己的 MIT 重写件,而语料仓本身已经公开。**这曾是本轮唯一一条后果不可撤回的
   未决问题,它是被证伪的,不是被回答的。** 留在这里不删:一个消失了的顾虑和一个被答掉的顾虑,
   下次读的人要能分清。
4. **`corpus samples` 排除的默认行为。** 建议默认**不**排除(默认分母不能悄悄变小),排除必须显式;
   但 `corpus score` 头部固定打印"本次含/不含哪些来源"。
5. **阈值怎么折成一个词。** Heeler 只有 exit 1/0,但有 policy 文件决定什么算 fail。
   建议**不是**用它的默认 policy —— 语料文档要的是"选那个对应你产品**实际行为**的阈值
   (拦住加载、让构建失败)",而默认 policy 不一定是那一档。所以:找它最接近"让构建失败"的那一档,
   把 policy 文件哈希记进元数据,并在成绩单上写明这是**我们选的**;拿不准就**跑两遍**
   (语料文档:"诚实的答案是两个阈值,就跑两遍")。
6. **Heeler 要不要进 `taxonomy/tools.yaml`?** 建议进,只为拿归类分(part 2);`dimension_map`
   **必须来自它自己公布的分类**,不是我们猜的 —— 拿不到就不写 `dimension_map`,归类报"不可测"
   而不是 0。**不写任何 `expect.heeler` 块**(见"不做什么")。
7. **维护面预算(`plan §10`:加一个面必须说出减哪个)。** 决定 1 让这笔账便宜了不少 —— 不再是
   一个新仓库(独立的 CI、LICENSE、README、发布流程),而是本仓库里一个目录。但它仍然是个面:
   adapter 跟着别人的 CLI 漂。建议的抵扣:它**取代** `docs/comparison.md` 的手写对照表
   (一个每次竞品发版都要手工刷新的面),换成可重跑的测量,并明确"按需跑、**永不进 CI**"
   (和 `make bench` 同一条纪律)。§10 点名的两个待减项(judge 的 provider 预设表、`inbox` 的
   zip 路径)**本轮不动**。这笔抵扣算不算,要人确认。
8. ~~**跨仓记账。**~~ **已决(2026-09-21):作废。** 它是"八条工作项在别的仓库"派生出来的问题,
   而决定 1 把十条有效工作项里的八条拉回本仓库,只剩 W9–W10 两条在语料仓 —— 这正是 P-005 W4 的先例
   覆盖的规模(证据行引用另一仓的提交与命令输出),`docs/process.md` 不需要改。
   **这条留在这里不删,因为它记着一个判断为什么不再需要回答。**
9. **隔离方式。** 建议 Docker(`--network none`、样本只读挂载、tmpfs 写层);没有 Docker 的机器
   **直接拒绝**跑标了"会执行被扫内容"的工具,不降级。Heeler 不在该名单上,不挡第一轮。

## 完成

**这是一次部分交付。** 十一条工作项里做完九条(W4 作废),**W6、W11 未做** —— 它们是 Heeler 那两条,
卡在装 `heelercli`、开账号、读 `/terms` 三件只有人能做的事上。先例:P-005 也是带着
"三条跨机器判据未验证,W4 待语料仓"进 `complete/` 的。

```
合入:PR <编号>(2026-09-21;sha 合入后用 git log --grep P-015 找)
发布:v0.12.0
证据:TestCheckRejects(14 条,含拒绝 `skipped` 这个下场)、TestCheckAcceptsAnExhaustiveLedger、
      TestTallyAddsUp(baselines/ledger,91.9%);
      TestFiresWhenNothingIsFlaggedOnALargeSurface、TestADeclarationTurnsAFailureIntoADisclosure、
      TestADeclarationForASurfaceWithFindingsIsAMistake、TestTheCrossoverIsTwentyTwo(baselines/tripwire,96.2%);
      TestValidateRequiresTheAttributionFields、TestYAMLRoundTripKeepsTheAttribution(baselines/run,100%);
      TestArgsCarriesEveryContainmentFlag、TestArgsIsAnArgvNotAShellString、
      TestAvailableIsARefusalNotADowngrade(baselines/isolate,93.8%);
      TestUnplaceableButReadableTreeIsScoredAndDisclosed、TestGateFiringIsNotAFailedRun、
      TestReadNothingOnlyMeansNoArtifactAtAll(baselines/adapter/aguard,69.3%);
      TestParseSourceFilters、TestAMistypedSourceIsRefused、TestReportExclusionAlwaysSaysSomething(语料仓);
      反向断言实跑:3412 条既有 verdicts 缺失 0 · 逐字节不同 0 · 新增 127;
      语料仓 make validate exit 0 且 `git grep -il heeler harness/` 0 命中;
      corpus samples --exclude-source cisco-mcp-scanner-evals = 3412 of 3539(少 127);
      账本 3539 scored · 0 no-verdict · 0 error = 3539/3539,skipped 0;
      6 个 fixture 首次运行全 pass,traverse-only-dir 靠 COV-000 过;
      数字变化:恶意召回 71/173 = 41% → 85/300 = 28%(分母诚实化,非退步),hard negative 4/19 不变,
      来源间极差 45 → 60 点(cisco 那一列第一次有了率:11%,14/127);
      良性误报 219 → 176 → 147(6.8% → 5.5% → 4.6%)—— **后两步是上游 P-012 与 P-013 的成果,不是本条的**,
      rebase 到 v0.11.0 之后重测并把基线换掉了(旧的那份与树对不上,已删);
      基线提交在 baselines/results/aguard/2026-09-22/(verdicts、ledger、fixtures、scorecard、run.yaml)
```

### `make verify` 的实际状态,逐条

四过两不过,**两条不过的原因都不是本次改动**,照实列:

| 闸门 | 状态 |
|---|---|
| `go vet ./...` | ✅ |
| `go test -race -cover ./...` | ⚠️ 除 `TestDocsRelativeLinksResolve` 外全绿;那条红是 gitignored 且未跟踪的 `_gitlocks/merge.extracted/` 里 27 条链接,把该目录移开即绿(已实测并还原)。**既存问题,与本次无关** |
| `golangci-lint run ./...` | ❌ **没跑** —— 这台机器没装 |
| `docs/rules.md` 无漂移 | ✅ |
| `check plugin --fail-on low` + 四条流程 skill 自扫 | ✅ |
| `go.mod` 仍是 `go 1.23.5` | ✅ |

### AI 自己不确定的点(review 时请重点看)

1. **`baselines/cmd/baseline` 覆盖率 0%。** 不变量都在各自的包里测,驱动是接线;但按仓库的测试标准
   这是个缺口,我没有假装它不存在。
2. **`isolate.Args` 目前没有任何消费者。** 包注释写明了这一点和理由(没有"会执行被扫内容"的 adapter)。
   如果你认为未用代码不该进主干,这一段应该退回。
3. **那 127 个样本该不该进头条分母,是定位问题,不是技术问题。** 我做的是"测了、标了、欠两个分母",
   没替你拍板。`direction §4` 说不扫 agent 应用源码,而 `check` 实测会读 —— 这两句的张力需要人来解。
4. **Snyk `agent-scan` 会执行被扫 MCP 配置**这条,我至今**只有二手说法,没有复核**。W8 的隔离整条
   建立在它之上;若不成立,隔离可以后置。
5. **`sparse-huge-config` 的判定弱于 fixture 的本意**:它要的是有界读取,我只能观察到终止。
