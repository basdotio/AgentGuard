<!-- SPDX-License-Identifier: MIT -->
# 035 — 把每一票的严重度记下来:三票只留第一票,判定为什么变了事后读不出来

- **来源**:`samples: 3` 方差实验(2026-09-29,PR #39,`baselines/results/aguard/2026-09-29-llm-gpt-4.1-mini-s3/`);plan §3.1 第 5 步第 ② 关
- **依赖**:无
- **分支**:`p/035-majority-severity`
- **改名**(2026-09-29,已决 7):原题「三票都同意的判断,严重度却只看第一票:进不进闸门成了抽签」。检查点在同一份 raw 上实测,
  多数票取法 224 个样本一个判定都不改,W3/W4 撤销,本条只交付票面

<!-- 目录即状态。 -->

## 问题

`samples > 1` 时,同一个问题问 n 次,`internal/judge/run.go` 的 `tally` 按"报了且证据落地"的票数决定这条发现**带不带权**
(多数 = n/2+1)。但带权之后它**是什么严重度**,取的是第一张同意票的:`f := votes[0]`。温度 0.8 下每一票的自评严重度本身是一次抽样,
于是一个三票全同意的判断,进不进闸门(折叠口径与 `--fail-on-llm` 都看严重度)取决于排在第一的那一次采样。

方差实验里,和 09-28 的 `samples: 1` 在同 100 个随机恶意样本上对比:

| | 个数 |
|---|---|
| 判官旗标丢失 | 3 |
| 其中三票全同意、只因第一票给了 medium 而没过 high | **3** |
| 定向良性里同样原因丢失 | 1(LLM-001 3/3,第一票 medium) |

这 4 个是"变了"不是"错了":第一票 medium 也可能是多数的看法。**说不清是哪一种,因为 raw 里只剩第一票的严重度**——
其余几票的自评在 `tally` 里就丢了。所以:

- `samples: 3` 的数字里混着一次与共识无关的抽签,而 plan 第 5 步第 ③ 关(全量 3,220 良性)要用 `samples: 3` 跑,会原样继承它;
- 这一抽签的大小现在**量不出来**,方差实验只能看到它发生过。

## 初步方向

两步:先让每条 `samples > 1` 的发现把所有同意票的严重度按采样顺序写进理由,重跑同一批 224 个,在同一份 raw 上算出
"第一票"和"多数票"两种取法各自的结果;量完再把 `tally` 的严重度改成多数票达到的那一档。只动 `internal/judge`,
规格 §5.2.1 ② 与 `docs/llm-judge.md` 双语对子各补一句。

## 设计

**"多数票达到的那一档"**:一组 n 次采样里,取**至少 majority(n) 票报到了或超过它**的最高严重度。它和带不带权问的是同一个问题——
"多数样本认为它至少这么严重"——只是把"报了"细到了档位:

| 同意票(按采样顺序) | n = 3 | 第一票取法 | 多数票取法 |
|---|---|---|---|
| medium, high, high | 3 of 3 | medium | **high** |
| high, medium, — | 2 of 3 | high | **medium**(只有 1 票 ≥ high) |
| high, high, medium | 3 of 3 | high | high |
| high, medium, low | 3 of 3 | high | **medium** |
| high, —, — | 1 of 3,不带权 | high | 不适用:不带权的发现照旧取第一票,只展示 |

三档之中(`clampSeverity` 只产出 low / medium / high),`3 of 3` 时它就是中位数,`2 of 3` 时是两票里较低的那个。
`samples: 1` 时 majority(1) = 1,结果就是那一票——**单次采样的行为按构造不变**。
`LLM-007` 的严重度由工具定死为 high(`barrierFinding`),每一票都一样,也按构造不变。

**票面**:`samples > 1` 时理由里现有的 `[k of n samples agreed]` 原样保留,后面另起一个方括号按采样顺序列出每张同意票的严重度,
例如 `[2 of 3 samples agreed] [severities: high, medium]`。不加 JSON 字段:票数现在就只在理由文本里,两样东西放在一处。

**为什么先量**:这 4 个样本在多数票取法下会不会回来,现有 raw 答不了(见「问题」)。W2 落地后重跑同一份
`samples.jsonl`(同一份判官配置,只换二进制,约 15 分钟),每条发现的全部票面都在新 raw 里,两种取法在**同一次运行**上对比,
不掺跨运行的抽样噪声。量完再决定 W4 落不落。

## 完成的判据

- ~~[ ] `TestConsensus_SeverityIsTheMajoritys`(`internal/judge/consensus_test.go`)钉住上表前四行:同意票按采样顺序给定时,
      带权发现的严重度等于"多数票达到的那一档"。W4 之前红在第 1、2、4 行(第一票取法给出 medium / high / high)~~
      **撤销**(已决 7):W3/W4 不做,严重度取法不变
- [x] `TestConsensus_VoteSeveritiesAreShown` 钉住票面:`samples: 3` 时理由含 `[k of 3 samples agreed]` 与
      `[severities: …]`,顺序即采样顺序;`samples: 1` 时两者都没有
- [x] 反向断言:`samples: 1` 不出票面(`TestConsensus_VoteSeveritiesAreShown` 第 4 例);不带权(1 of 3)的发现照样出票面、仍写
      "carries no weight";`TestBarrier_ConsensusApplies` 与 `TestConsensus_MajorityDecidesWeight` 不改一行仍绿;
      `TestApply_EffectiveNeverExceedsOverall` 仍绿。(原写"`samples: 1` 的严重度等于那一票"是给 W4 的,W4 撤销,严重度取法没动)
- [x] 实测:W2 的二进制重跑那 224 个,报出同一次运行上两种取法的差别——严重度变了的带权发现有几条、按折叠口径翻了 verdict 的样本
      按层各几个(随机恶意 / 随机良性 / 定向);数字进「完成」。见「检查点」:2 条换档,0 个翻转
- [x] 规格 §5.2.1 ② 补一句票面(已决 7:不写严重度取法,它没变);`docs/llm-judge.md` 与 `docs/llm-judge.zh-CN.md` 的 Consensus 一节同步;`make verify` 绿

## 不做什么

- **同一组里各票的规则不一致**(第一票说 LLM-001、第二票说 LLM-003):仍由第一票命名这条发现、决定它是不是 `LLM-009`。
  ~~重跑里只数一下有多少组这样,记进「完成」~~ **数不了**(2026-09-29 更正):W2 的票面只记严重度,不记每票的规则,
  raw 里看不出一组里各票是不是同一条规则。要数就得把规则也写进票面,那是另一件事,见未决问题 8
- 采样温度 0.8、多数门槛的公式、`advisoryOnly`、`clampSeverity`、证据落地:一个不动
- 不给 `model.Finding` 加字段,JSON / SARIF 的结构不变
- hook 配置上那 3 个 LLM-008 稳定误判:另一件事
- plan 第 5 步第 ③ 关(全量良性)不在这里跑
- `baselines/` 的驱动与适配器不动

## 不能说什么

- 不说"`samples: 3` 更准了"或"误报降了":这里只改一个多数已经同意的判断带着哪一档严重度,不改模型对不对
- 重跑之前,不说"那 3 个样本找回来了"——多数票也可能就是 medium(重跑后:它们回来是跨运行的抽样,不是取法)
- 不说"多数票取法没用":它在这 224 个、这一个模型上一个判定都没改,别的样本和模型上没量过
- 不说判官"严重度稳定":票面记录的是同一组内的分散,跨运行的不在其中

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 票面测试(先红) | `judge: test — every agreeing vote's severity is in the reason, in sample order (P-035)` |
| 2 | 票面落地 | `judge: a sampled finding names every agreeing vote's severity (P-035)` |
| — | **检查点**:你用 W2 的二进制重跑 224 个;我在新 raw 上对比两种取法,数字写进未决问题,你决定 W3/W4 做不做 | — |
| ~~3~~ | ~~多数票测试 + 反向断言(先红)~~ 撤销(已决 7) | `judge: test — an escalated finding carries the severity a majority reached (P-035)` |
| ~~4~~ | ~~`tally` 取多数票那一档~~ 撤销(已决 7) | `judge: an escalated finding carries the severity a majority reached, not the first vote's (P-035)` |
| 5 | 规格与双语文档:票面 | `docs: spec §5.2.1 and the llm-judge pair show every vote's severity on a sampled finding (P-035)` |
| 6 | 重跑结果进 `results/` | `baselines: the P-035 rerun under results/ — every vote's severity, first vote vs majority on one raw (P-035)` |
| 7 | plan 第 5 步记下跨运行方差 | `plan: step 5 gate ② measured — two samples:3 runs disagree on 7% of malicious (P-035)` |

## 未决问题

1. **严重度取哪一档?** **建议**:上面的"多数票达到的那一档"。另两种:同意票的中位数(2 票时没有定义,得再定一条规则);
   同意票里的最高(那等于把抽签换成往上偏的抽签——判官只许单向升级,不该在它自己的档位上也往上挑)。
   **已决(2026-09-29)**:按建议。
2. **不带权的发现(1 of 3)严重度怎么取?** **建议**:照旧取第一票。它只展示、不进任何分数和闸门,改它没有可量的后果。
   **已决(2026-09-29)**:按建议。
3. **标题、理由、证据取哪一票?** **建议**:照旧取第一票。每张同意票都已过证据落地,只换严重度是最小的改动;
   代价是理由文字可能出自一张给了别的档位的票,票面那一括号会把这点摆在读者眼前。
   **已决(2026-09-29)**:按建议。
4. **票面放哪、什么格式?** **建议**:理由里另起一括号 `[severities: high, medium]`,不动已有的 `[k of n samples agreed]`,
   不加 JSON 字段。已有的读法(测试、方差实验的折算脚本)都认前一个括号。
   **已决(2026-09-29)**:按建议。
5. **先量再落?** **建议**:是。W2 落地后停一次,你重跑 224 个(约 15 分钟,付费 API);两种取法差得少,W3/W4 可以不做,
   本条只交付票面,改名为"把每一票的严重度记下来"。
   **已决(2026-09-29)**:按建议。
6. **重跑结果进不进 `baselines/results/`?** **建议**:进,新目录 `2026-09-30-llm-gpt-4.1-mini-s3-votes/`(日期以实际为准)。
   它是本条和第 ③ 关的一手证据,而且是第一份带全部票面的 raw;和前三份一样 opt-in 进 `.gitignore`。
   **已决(2026-09-29)**:按建议。

### 检查点(2026-09-29,W2 之后的重跑)

W2 的二进制(`fc6b179`)重跑同一份 224 个、同一份判官配置,08:43–08:57Z,1,769 次调用,失败 3 次。每条发现的全部票面都在 raw 里,
两种取法在**同一次运行**上对比:

| | 随机恶意 | 随机良性 | 定向良性 | hard negative |
|---|---|---|---|---|
| 带权发现(规则不是 LLM-009) | 82 | 21 | 15 | 6 |
| 三票严重度不全一样 | 6 | 0 | 5 | 0 |
| 多数票 ≠ 第一票 | 1(往上越过 high) | 0 | 1(往下越过 high) | 0 |
| **判定翻转的样本** | **0** | **0** | **0** | **0** |

那两条换了档的发现,所在样本另有别的发现撑着判定。第一次运行里因第一票 medium 丢掉的 4 个,这次的票面:
`base64-obvious` 3/3 [high, high, high];`self-rewrite-update` 3/3 [high, high, high];`clean-manifest-nested` 3/3 [medium, medium, medium];
`hashi…terraform-mcp-as-code` 3/3 [high, medium, high]。

**两次 `samples: 3` 运行之间**(同配置,只差票面记录)的差别才是那 4 个的来源:

| 判官自己的旗标 | 两次都有 | 只第一次 | 只第二次 | 不一致 |
|---|---|---|---|---|
| 随机恶意 100 | 58 | 3 | 4 | 7% |
| 随机良性 100 | 0 | 0 | 1 | 1% |
| 定向良性 5 | 4 | 0 | 1 | — |
| hard negative 19 | 0 | 0 | 0 | 0% |

随机良性那 1 个是 `spacehendrix`:第一次 LLM-007 1/3 被共识压下,这次 2/3 又升级——PR #39 里"共识去掉了 1 个良性旗标"在第二次运行里没复现。
在两次里至少被报过一次的 105 个问题(样本 × 规则,不含 LLM-007/009)上,带不带权两次一致的 100 个(95.2%)。
**一次运行内的三票不像是相关的**:同一次运行里任两票严重度不同的比例 7.7%(586 对里 45),两次运行第一票不同的比例 9.2%(109 个里 10),
差在噪声里——对话里一度说"三票可能不独立",按这两个数不成立。

7. **W3/W4 做不做?** **建议**:不做。同一份 raw 上多数票取法一个判定都不改(0 / 224);那 4 个样本来自跨运行的抽样,
   多数票取法碰不到它。本条按已决 5 交付 W2(票面)与这次重跑的结果目录(已决 6),标题改为"把每一票的严重度记下来";
   W5 只写票面的格式,不写严重度取法(它没变)。跨运行 7% 这个数交给 plan 第 5 步:它才是"同一样本多轮方差"。
   **已决(2026-09-29)**:按建议。
8. **票面要不要也记每票的规则?** **建议**:这里不做。没有数据说明规则分歧大到值得(上表带权问题两次一致 95%);
   等第 ③ 关或别的实测给出理由再单开。
   **已决(2026-09-29)**:按建议。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-035 找)
发布:v0.15.0
证据:TestConsensus_VoteSeveritiesAreShown(internal/judge/consensus_test.go):W1 时红 3 / 4(三个 samples:3 用例缺 [severities: …]),
     W2 后绿 4 / 4;第 4 例 samples:1 不出票面为反向断言;TestConsensus_MajorityDecidesWeight、TestBarrier_ConsensusApplies、
     TestApply_EffectiveNeverExceedsOverall 不改一行仍绿;
     重跑(fc6b179,同 224 个、同判官配置,1,769 次调用、失败 3):带票数的发现 233 条全部带 [severities:],第一项与发现自身严重度
     233 / 233 一致;同一份 raw 上多数票 vs 第一票:带权发现 124 条里换档 2 条,判定翻转 0 / 224;
     两次 samples:3 运行之间判官自己的旗标差 7 / 100(随机恶意)、1 / 100(随机良性)——
     baselines/results/aguard/2026-09-29-llm-gpt-4.1-mini-s3-votes/compare.txt;make verify: all gates passed
```
