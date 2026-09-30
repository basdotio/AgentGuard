<!-- SPDX-License-Identifier: MIT -->
# 019 — 判官把"MCP 包没钉版本"当高危送进闸门,真实配置每百个多拦一个

- **来源**:新发现(2026-09-25,判官在语料子集上实测);plan §3.1 判官那一节的前置
- **依赖**:无(验收用 P-015 的 `make bench` 加判官透传;透传本身另行决定要不要进仓)
- **分支**:`p/019-llm-009-advisory-only`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`--llm` 打开后,判官对每个 MCP 服务器配置问一个问题:来源、是否 pin、传输方式、凭证(`LLM-009`,维度 5)。
它的答案和其他八个问题走同一条路:证据在原文里找得到、多次采样多数同意,就打上 `Escalates`,`--fail-on-llm`
和 `overall_effective` 都认。

2026-09-25 用 glm-5.3-flash、`samples: 1` 跑了 819 个样本(300 恶意 + 19 hard negative + 500 良性,良性按面分层抽),
`LLM-009` 是**唯一一条在良性上升级到 high 的判官规则**:

| 类 | n | 判官多拦的样本 | 其中只靠 `LLM-009` 的 |
|---|---|---|---|
| 良性 | 500 | 7 | **5**(12 条发现,全在 `.mcp.json`) |
| hard negative | 19 | 1 | **1**(`hn-conn-starrocks`) |
| 恶意 | 300 | 64 | 2(另 4 个静态或别的判官规则已经拦下) |

五个良性样本的理由都是同一句话的变体:"`npx -y xxx@latest`,没钉版本,拿到了凭证"。这在真实 MCP 配置里是常态,
几乎每份官方教程都这么写;P-016 为同一个理由把 `enableAllProjectMcpServers` 判为不做(387 份真实配置里 34 份开着)。
模型没有看错内容,它是把"不够规范"评成了"恶意"。规格 §5.2.1 给判官的定位是对抗式分析层,补静态看不见的**意图**;
供应链规范性是能写成词表的东西,按 §5.2 的判据该归静态,而静态那边已经决定不拦。

后果:良性多误判从 2/500(0.4%)变成 7/500(1.4%),越过 plan §3.1 给判官进闸门定的 ≤ 0.5% 门槛;hard negative
从 4/19 变 5/19。**这条规则不改,判官就没有资格谈 `--fail-on-llm`。**

恶意侧只靠它的两个:`mal-conn-mcp-autorun-bypass`(P-016 明确不做的形状)和 `mal-conn-mcpoison-post-approval-swap`
(`.mcp.json` args 里的 `bash -i >& /dev/tcp` 反弹 shell,plan §3.1 第 2 步第二组的静态规则会拦,不该靠判官)。

## 初步方向

`LLM-009` 的发现照常报告,但**永远不打 `Escalates`**:读者看得到"这个包没钉版本",它不再让 `overall_effective` 升分、
不进 `--fail-on-llm`。改动在 `internal/judge`:给规则加一个"只提示"属性,`tally` 打标记时跳过;`docs/llm-judge*.md`
和 `docs/rules.md` 的速查表补一句。不改判官的问题本身,不改 `clampSeverity`(模型给的严重度仍原样显示,只是没有重量)。

验收用同一份 819 子集和同一份原始输出重折叠:良性多拦 7 → 2,hard negative 5 → 4,恶意 151 → 149;
反向断言:`LLM-003`/`LLM-001`/`LLM-007` 的升级数一个不少(95 / 36 / 10)。

<!-- ===== design 段 ===== -->

## 完成的判据

- [ ] `TestMCPConfig_NeverCarriesWeight`(`internal/judge`)钉住:脚本客户端对 `ModeMCPConfig` 三次采样全部 flagged、严重度 high、
      引文可落地,`Run` 之后该 artifact 上**有** `LLM-009` 发现,`Escalates == false`,`Why` 末尾带"只提示、不加权"的说明;
      `score.Apply` 后该 artifact 的 `ScoreEffective == Score`。
- [ ] 反向断言(同一测试内):同样的三次采样对 `ModeIntent` 仍产出 `LLM-001` 且 `Escalates == true`;`ModeInjection` 的
      `LLM-007` 仍 `Escalates == true`。现有 `TestConsensus_MajorityDecidesWeight` 继续绿。
- [ ] 真机复测:用改后的二进制、glm-5.3-flash、`samples: 1`,只跑 2026-09-25 那次里**只靠 `LLM-009` 翻 verdict 的 8 个样本**
      (良性 5、hard negative 1、恶意 2),每个的 `.mcp.json` 上仍报 `LLM-009`、`escalates` 字段缺席、`score_effective == score`。
      819 个全量不重跑:按同一份原始输出重折叠,良性多拦 7 → 2,hard negative 5 → 4,恶意 151 → 149,`LLM-003`/`LLM-001`/
      `LLM-007` 的升级数 95 / 36 / 10 不变。
- [ ] `make docs` 后 `docs/rules.md` 的 `LLM-009` 行写明"只提示";`make verify` 绿。

## 不做什么

- 不改 MCP 配置那趟的提示词、问题、触发条件(`planFor` 里 `KindMCP` 的分支)。判官照样问,照样报。
- 不改 `clampSeverity`:模型给的严重度原样显示,只是没有重量。不把 `LLM-009` 封顶到 medium(见未决 1)。
- 不动 `score.Escalating` 和 `score.Apply`:资格仍然由判官在 finding 上决定,分数那边只读标记。
- 不给其他八条判官规则加"只提示"属性;`LLM-008` 在良性上的 2 个升级(强杀端口进程、开会话装软件)保留。
- 不加配置项让用户把 `LLM-009` 重新变成加权(见未决 2)。
- 不动语料、不动 `baselines/`(测量用的透传改动另行决定,见未决 4)。

## 不能说什么

- 不说"判官误报 0.4%":对外数字必须带模型名、`samples`、样本数(500 良性子集)和日期,按面带 n。
- 不把静态和判官的召回合成一个数;判官那列永远单列。
- 报告里 `LLM-009` 的措辞是"只提示、不加权",不是"安全"或"已忽略":没钉版本的包仍然是值得看一眼的事实。

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 写 `TestMCPConfig_NeverCarriesWeight`,跑红:红的原因是 `LLM-009` 带了 `Escalates` | `judge: test pins LLM-009 as advisory-only, red until the rule stops escalating (P-019)` |
| 2 | `internal/judge`:规则级"只提示"集合,`tally` 打标记时跳过,`Why` 追加说明 | `judge: LLM-009 never escalates — unpinned-package configs are shown, not gated (P-019)` |
| 3 | `hack/gen-rules` 的 `LLM-009` 描述、`docs/llm-judge*.md` 速查表、spec §5.2.1 加"规则级资格"一句、`.claude/rules/judge.md` 一行;`make docs` | `docs: LLM-009 is advisory-only, the spec's escalation bar gains a per-rule clause (P-019)` |
| 4 | 8 个样本真机复测,头部和 `LLM-009` 行贴进未决问题 | (不产生提交;证据进「完成」) |

## 未决问题

1. **机制选"永不升级"还是"严重度封顶 medium"?** 建议前者。封顶不等于没重量:`overall_effective` 按维度取最重罚分,
   一条 medium 的合格发现照样把分拉下来,子集上的 5 个良性还是会被 `--fail-on-llm medium` 拦。而且封顶会丢掉模型的
   严重度信息,读者看不出"它觉得这个有多严重"。
2. **要不要给用户一个开关把 `LLM-009` 重新变成加权?** 建议不要。现在只有一条规则需要这个属性,先硬编码在判官里;
   第二条出现再抽象成配置。开关还会让"判官误报 ≤ 0.5%"这个数变成"取决于配置"。
3. **`Why` 的后缀怎么写?** 建议英文 `[advisory only — this rule never carries weight]`,和现有的
   `[2 of 3 samples agreed]` 同一风格,放在采样说明之后。
4. **测量用的 `baselines/` 透传改动(adapter 加 `ExtraArgs/ExtraEnv/Timeout`,driver 加三个 flag)要不要随本条进仓?**
   建议不进:本条只改判官;透传是测量工具,按仓库惯例单独问、单独提。W4 复测在本地用它跑,披露即可。
5. **W4 复测要用 GLM 的 key。** 建议做:8 个样本几分钟就完;key 只走环境变量 `ZHIPU_API_KEY`,不进任何文件。
   如果 key 已经吊销,W4 改为只给重折叠的数字,并在「完成」里写明没有真机复测。

**已决(2026-09-25)**:五条全部按建议 —— 永不升级、不加开关、英文后缀、透传不进仓、做真机复测。

## 完成

```
合入:[PR #20](https://github.com/basdotio/agent-guard/pull/20)(2026-09-25;sha 合入后用 git log --grep P-019 找)
发布:v0.12.0
证据:TestMCPConfig_NeverCarriesWeight(internal/judge/advisory_only_test.go);819 子集重折叠 良性多拦 7 → 2、hard negative 5 → 4、恶意 151 → 149 = LLM-009 不再升级,LLM-003/001/007 升级数 95/36/10 不变;真机复测 8 个样本(glm-5.3-flash,samples 1)LLM-009 全部报告、escalates 缺席、score_effective == score;反向断言在同一测试的 LLM-001/LLM-007 段
```

真机复测的 8 个:`ben-conn-superamped-ai-marketing-skills-mcp`、`ben-conn-paldom-databricks-apps-fastapi-starter-mcp`、
`ben-conn-duhu2000-financial-services-qcc-mcp`、`ben-conn-spacehendrix-clauder-mcp`、`ben-conn-motleyai-motleycrew-mcp`、
`hn-conn-starrocks`、`mal-conn-mcp-autorun-bypass`、`mal-conn-mcpoison-post-approval-swap`。跑法:本地 `baselines/` driver 加未提交的
`--llm` 透传(未决 4),key 走 `ZHIPU_API_KEY`。恶意 2 个按预期不再翻 verdict。
