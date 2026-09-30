<!-- SPDX-License-Identifier: MIT -->
# 034 — 语料标签里有"该工具不检测"这个字段,评分器却从不读它:两道召回门槛只能靠估算

- **来源**:P-023 已决 1、2(评分器读 `out_of_scope` 另开一条);plan §3.1 门槛表第三条
- **依赖**:无;语料仓改动须先于本仓 `make bench` 的改动合入
- **分支**:`p/034-scorer-out-of-scope`(本仓);语料仓另开同名分支

<!-- 目录即状态。 -->

## 问题

语料仓的标签格式早有逐工具字段 `expect.<工具>.out_of_scope`(`harness/internal/label/label.go:268`),注释写着
"该样本离开这个工具的召回分母,并在报告里点名"。校验也写好了(只许打在恶意样本上)。但:

- `corpus score` 不知道自己在给哪个工具打分:命令只收 verdicts 文件,verdict 行里没有工具名,`score.Score` 也没有工具参数;
- 召回那一段从不读 `OutOfScope`;3,539 个标签里**零个**用了它。

后果:plan 门槛表里"朴素 + 规避召回"与"语义类召回"两行的当前值是**按分层计数估的**,不是 `make bench` 的输出;
M1.6(对外发数字)的前提就是这个分母能由评分器直接给出。

## 实测(2026-09-29,语料 4f964622,基线 2026-09-29d)

| | 数 |
|---|---|
| Cisco 服务端源码样本 | 127,aguard 抓到 **21** |
| 非 Cisco 恶意 | 173,aguard 抓到 89 |
| truth 里 `evasion: binary-container` 的恶意 | 4,全部漏(即 plan 的"D 类 4 个") |
| Cisco 标签里能区分"服务端应用漏洞"与"agent 制品攻击"的字段 | **无**(全部来自上游同一类别,无 technique 字段) |

## 初步方向

- 语料仓:`corpus score -tool <id>`,按该工具的 `out_of_scope` 扣召回分母,**永远同时打印全量与扣除后两个分母**;
  扣掉的样本逐个点名。不给 `-tool` 时行为与现在逐字节相同。
- 本仓:`make bench` 传 `-tool aguard`。
- aguard 的声明写在哪、按样本还是按来源划,设计阶段定(见问题清单)。

**值得设计**:人 2026-09-29"开 P-034 做语料仓评分器 out_of_scope"。

## 已决(2026-09-29,人)

1. **按来源划,不按样本划。** aguard 声明不检测的是:整个 `cisco-mcp-scanner-evals` 来源(MCP 服务端实现源码;`direction` §4 不扫 agent
   应用源码),加 truth 里 `evasion: binary-container` 的样本(容器里的载荷,aguard 不拆容器)。
   **这改了 P-023 已决 1 的写法**(原为"标出 Cisco 里按定位碰不到的约 106 个")。改的理由是实测:aguard 现在恰好抓到 Cisco 的 21 个,
   那 106 个正是它漏掉的全部——按样本标,Cisco 召回会是 21/21,分母按"我们漏了什么"来划,对外说不清。
   按来源划是机械的、可复算的,代价是 aguard 在 Cisco 里抓到的 21 个也不进扣除后的分母(300 分母里照旧在)。
2. **语义类另开。** 语义类是工具中立的 truth 字段,要逐样本读,单独一条;本条只做评分器机制和 aguard 的声明。

## 设计

- **声明写在 `taxonomy/tools.yaml` 的工具条目里**,是选择器而不是逐样本标记:
  ```yaml
  out_of_scope:
    - source: cisco-mcp-scanner-evals     # 匹配 populationOf(label),即 derived 样本的上游条目 id
      reason: "…"
    - evasion: binary-container           # 匹配 truth.evasion 里任一项
      reason: "…"
  ```
  不写进 127 个 Cisco 标签:它们是 `corpus derive` 生成的,重新派生会覆盖。标签上已有的逐样本 `expect.<工具>.out_of_scope`
  也一并生效(注释承诺了,没实现过)。
- **`corpus score -tool <id>`**:不给 `-tool` 时输出与现在**逐字节相同**;给了,在现有报告之后**追加**一段:
  逐条列出选择器、各自扣掉多少、理由(样本少于 10 个时列出 id),再给
  `malicious, all: N of 300` 与 `malicious, in scope: M of K (点估计, [Wilson 区间])` 两行。
  **现有各段一律不改**,所以全量分母在结构上一定还在——"任何发布给两个分母"由输出格式保证,不靠人记得。
- **校验**:`taxonomy.Validate` 要求每个选择器恰有 `source`/`evasion` 之一、`reason` 非空、`evasion` 在词表里;
  `corpus score -tool` 时 `source` 必须是 manifest 里的条目、`<id>` 必须是登记过的工具,否则退出 2。
- **中立性**:harness 代码里不出现任何工具名(选择器是通用的);aguard 的名字只在它自己的 `tools.yaml` 条目里。
- **本仓**:`make bench` 调 `corpus score -tool aguard`。

## 完成的判据

- [ ] 语料仓 `TestScore_OutOfScope`(`harness/internal/score/outofscope_test.go`):来源选择器、evasion 选择器、逐样本 `expect` 三种都扣分母;
      一个样本同时命中两个只扣一次;没有 verdict 的样本不进任何分母;不给工具时结果为空
- [ ] **反向断言**:`corpus score` 不带 `-tool` 的输出与改动前**逐字节相同**(对 aguard 2026-09-29d 的 verdicts 跑 `cmp`)
- [ ] `taxonomy` 校验拒绝:两个键都写、都不写、`reason` 空、未知 evasion;`corpus score -tool` 拒绝未知工具与未知来源(退出 2)
- [ ] 语料仓 `make validate` 与 `go test` 绿(含中立性检查)
- [ ] 本仓 `make bench` 的成绩单出现扣除段:**`malicious, in scope: 89 of 169`**(300 − 127 − 4),全量 `110 of 300` 仍在;verdicts 与 2026-09-29d 逐字节相同
- [ ] 本仓 `make verify` 绿

## 不做什么

- **不做语义类**(已决 2)
- **不改任何检测代码**:aguard 的 verdicts 一个都不变
- **不按样本挑 Cisco**(已决 1)
- **不改现有成绩单的任何一行**;扣除段只追加
- **不给 cc-audit、skill-scanner 写声明**:声明是工具自己的主张,我们替别人写就是替别人划分母

## 不能说什么

- **不写"aguard 召回 53%"而不带"扣除 131 个自己声明不检测的样本"**;任何引用都同时给 `110 of 300`
- **不写"排除了测试集污染"**:扣掉的是 aguard 声明不做的面,不是脏数据
- 门槛"朴素 + 规避 ≥ 90%"在这个分母上**仍够不到**:语义类还在里面,要等下一条

## 工作项

| W | 仓 | 一句话 |
|---|---|---|
| 1 | 语料 | `outofscope_test.go` + 校验测试 + 不带 `-tool` 逐字节不变的测试;先红 |
| 2 | 语料 | `taxonomy.Tool.OutOfScope` 选择器与校验;`score.OutOfScope`;`corpus score -tool`;追加段 |
| 3 | 语料 | `tools.yaml` 的 aguard 条目写两条声明 |
| 4 | 本仓 | `make bench` 传 `-tool aguard`;plan 门槛表那一行改成按来源、写实测值 |
| 5 | 本仓 | 语料仓合入后跑 `make bench`,基线进 `baselines/results/aguard/2026-09-29e/`(`corpus_commit` 须是语料仓 main 上可解析的提交) |

## 顺序

语料仓的改动先走它自己的 PR、先合;本仓 `make bench` 才能带 `-tool`。基线最后跑,保证 `run.yaml` 里的 `corpus_commit` 在语料仓 main 上。

**实现中攒下(2026-09-29)**

- **scorecard 由驱动写,不由 Makefile 那一行写。** `baselines/cmd/baseline` 自己跑 `corpus score` 并把输出存成 `scorecard.txt`,
  所以只改 `make bench` 的终端那行不够。加了驱动 flag `-score-tool`,**默认空**:其他工具的成绩单逐字节不变,
  也避免对语料仓没登记的工具(比如 heeler)退出 2。`make bench` 传 `-score-tool aguard`,README 的运行命令同步。
- 声明了但一个都没匹配到的选择器照样列出(计数 0):那正说明工具的主张在这批样本上不覆盖任何东西,读者该看到。
- 语料仓那边经 [agent-artifact-corpus PR #1](https://github.com/basdotio/agent-artifact-corpus/pull/1) 合入(`97e5af00`),
  基线在合入之后跑,`corpus_commit` 可在语料仓 main 上解析。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-034 找);语料仓 PR #1 已合入(97e5af00)
发布:v0.15.0
证据:语料仓 TestScore_OutOfScope / TestScore_OutOfScopeWithoutAToolIsEmpty(harness/internal/score/outofscope_test.go)、
     TestValidateOutOfScopeSelectors(harness/internal/taxonomy)、TestScoreArgs / TestResolveOutOfScope(harness/cmd/corpus);
     W1 在实现前编译失败(OutOfScopeSelector / parseScoreArgs 未定义),实现后绿;语料仓 make validate exit 0 且输出不变,go test -race 绿;
     反向断言:不带 -tool 的 corpus score 与 origin/main 旧评分器输出逐字节相同(61 行,cmp);
     本仓 TestScoreArgsCarryTheDeclaringTool(baselines/cmd/baseline/scorecard_test.go);
     make bench(corpus 97e5af00):malicious, all 110 of 300、malicious, in scope 89 of 169 (53%, [45, 60]);
     verdicts / ledger / fixtures 与 2026-09-29d 逐字节同 —— baselines/results/aguard/2026-09-29e/;make verify: all gates passed
```
