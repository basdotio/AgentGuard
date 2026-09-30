<!-- SPDX-License-Identifier: MIT -->
# 011 — 语料仓建好了,aguard 却没有东西能把它跑起来,误报率仍是一句空话

- **来源**:新发现(2026-09-21,对话提出);承接 [corpus-benchmark.zh-CN.md](../../corpus-benchmark.zh-CN.md) §4.3 与 plan M1.5。
  先以本地工具形态做出并用它跑出 P-010 的全部对照数字,领导 2026-09-21 同意进仓后补成 proposal
- **依赖**:无(P-010 用它验收,但代码互不依赖)
- **分支**:`p/011-corpus-runner`

## 问题

`agent-artifact-corpus`(本机 `../agent-artifact-corpus`,3,539 个带标签样本)把协议钉死了:`corpus samples` 出工作清单,
`corpus score` 把 verdict 算成召回、误报和覆盖;它**不运行任何扫描器**,并用 validate 的中立性检查禁止自己的代码出现任何
扫描器的 id。"怎么调用 aguard、怎么读它的输出、怎么把样本铺成它认的布局",归扫描器仓库。本仓库这一半不存在,后果有三:

1. **误报率至今没有分母。** `corpus-benchmark` §3.0 已把"本机 952 个 skill 的 12.9%"撤回成"一台机器的安装历史,n=7 个来源"。
   2,480 个真实 SKILL.md、353 个真实 MCP 工具目录、387 个真实 settings.json 就在隔壁目录里,没有一个被扫过。
2. **直接 `aguard check <样本目录>` 得到的是错的数。** 同一个良性 hooks 样本,`check` 当普通目录扫报 `FS-003` high(误报);
   按 `~/.claude` 布局铺好再 `scan --root`,报 `PERM-006` medium。high 阈值下 verdict 反转。
3. **两边的 `mcp` 不是一回事。** 语料的 `mcp` surface 是工具目录(`tools.json`)和服务器源码(`.py`);aguard 的 `mcp` kind 是
   `.mcp.json`,对应语料的 `connector`。127 个 Cisco 服务器源码样本在静态输入模型之外(direction:不扫 agent 源码),必须单独报出。

## 初步方向

`hack/corpus-runner/`:读 `corpus samples` 的 JSONL,把每个样本**按内容**铺进伪造的 `<home>/.claude`(`.claude/` 作 root;
`.mcp.json` 放 home;`SKILL.md` 树进 `skills/<id>/`;`tools.json` 合成 Claude Desktop 会话缓存喂 connector 收集器;单个松散 `.md`
作 `CLAUDE.md`),用隔离的 HOME/XDG_CONFIG_HOME exec `bin/aguard scan --json --no-reputation --inbox off`,按 `score.Deterministic`
加阈值这条闸门谓词折叠成一个词,输出 verdicts JSONL。铺不进去的不发 verdict(评分器报 uncovered),原因分布打到 stderr,
另打各类样本上"哪条规则带来了拦截"的计数。`make bench` 串三步。

## 完成的判据

- [x] `TestStagePlacesEachKindWhereAguardLooks`:settings → `<root>/settings.json`;`.mcp.json` → `<home>/.mcp.json`;`SKILL.md` →
  `<root>/skills/<id>/`;`tools.json` → 会话缓存且 tools 逐项相等;单个松散 `.md` → `CLAUDE.md`;只有 `.py` → 不可铺放且原因非空。
- [x] `TestVerdictFollowsTheGatePredicate`:static、dim≠0 的 high 在 high 阈值下 malicious、critical 下 benign;LLM high 与 dim-0
  high 都是 benign;dimensions 只含 `tools.yaml` 映射得到的名字。
- [x] 反向断言 `TestEndToEndAgainstRealBinary`:`go build` 出的真二进制,`curl … | sh` 的 hook 样本**仍**是 malicious,纯 allow 列表是 benign。
- [x] 真机:`make bench` 一次跑完 3,539 个,coverage 行 "127 had no verdict" = stderr 的 not placeable 127,scan errors 0,约 20 s。
- [x] `make verify` 绿。

## 不做什么

- 不改任何规则、severity、`internal/collect`、`internal/detect`(那是 P-010 这类各自的 proposal)。
- 不写 `docs/benchmark.md`,不在 README 或任何用户可见文档里发数字。
- 不把 runner 放进 corpus 仓(其中立性检查禁止),本仓 CI 不 checkout corpus,`make bench` 只给维护者机器。
- 不 vendoring 语料,不加 Go 依赖。
- 不给 Cisco `.py` 样本发任何 verdict。
- 不做注入故障 fixture 与 `expect.aguard` 规则级核对(第二阶段)。

## 不能说什么

- 任何"aguard 误报率是 X%"的对外陈述。产物是按来源的若干行,引用必须带 n、阈值、"信誉库关闭"、uncovered 数,只进 `docs/planning/`。
- 不把语料 `mcp` 一列的低召回写成漏检。
- stderr 里"某规则在良性集上带来 N 次拦截"是**计数**不是率。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 摆放、折叠、真二进制三组测试,对桩先红 | `hack: corpus-runner tests — placement, gate predicate, real binary (P-011)` |
| 2 | runner 实现 | `hack: corpus-runner turns corpus samples into verdicts corpus score can grade (P-011)` |
| 3 | `make bench`;CLAUDE.md 与 `corpus-benchmark` 的过期路径改掉并指向 runner | `build: make bench runs aguard over the corpus; planning doc points at the runner (P-011)` |

## 未决问题

1. **进程内调用还是 exec 二进制?** `cmd/aguard.analyze` 在 package main 不可导入,复刻四步会漂;exec 测的是发货的闸门。
   `hack/reputation-refresh`、`hack/measure-rules` 已是先例。**已决(2026-09-21)**:exec,`make bench` 依赖 `build`。
2. **摆放按标签 `kind` 还是按内容?** benign/mcp 里 353 个 `tools.json` 的 kind 是 `skill`。**已决(2026-09-21)**:按内容;`kind` 只作诊断。
3. **instruction surface 的松散 `.md`。** **已决(2026-09-21)**:`<root>/CLAUDE.md`。
4. **阈值与信誉库。** **已决(2026-09-21)**:默认 high;`BENCH_THRESHOLD=medium` 另跑一组;`--no-reputation`。
5. **要不要走 proposal。** 领导 2026-09-21 先定"只是测试工具,不提交",同日同事要看代码后改为提交。**已决(2026-09-21)**:补成 proposal 进 PR;
   它是 `.go` 改动,不在例外条款里。

## 完成

```
合入:PR #10 https://github.com/basdotio/agent-guard/pull/10(2026-09-21;sha 合入后用 git log --grep P-011 找)
发布:v0.11.0
证据:TestStagePlacesEachKindWhereAguardLooks、TestVerdictFollowsTheGatePredicate、TestVerdictsAreWrittenSortedAndParseable、
      TestEndToEndAgainstRealBinary(hack/corpus-runner/main_test.go);make bench 真机 2026-09-21:samples 3539 · scanned 3412 ·
      not placeable 127(= corpus score 的 127 had no verdict)· scan errors 0;它跑出的对照数字见 P-010「实现阶段记录」
```
