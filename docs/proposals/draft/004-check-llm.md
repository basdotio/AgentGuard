<!-- SPDX-License-Identifier: MIT -->
# 004 — 装前检查不能用判官,CI 用户只能走 scan --root 的绕路

- **来源**:`check` 按规格恒静态,装前检查一个下载来的 skill 时用不上判官;`--llm` 本来就是显式开关,`scan` 已经对同样不可信的
  Downloads 内容提供它。移植自旧仓 agent-guard 的 P-047(私有仓)
- **依赖**:无
- **分支**:`p/004-check-llm`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`aguard check <path>` 是在 PR 上审一个 skill / 插件 / zip 的那条命令(默认 `--fail-on high`)。它没有 `--llm`:
在 `main`(`dec64ca`,v0.18.0)上 `aguard check ./x --llm` 是 cobra 的 `error: unknown flag: --llm`,退出码 2;`--fail-on-llm` 同样不存在
(`error: unknown flag: --fail-on-llm`,退出码 2)。`cmd/aguard/main.go:683` 写着 "`check` is static-only by contract (spec §3)",
`main.go:656` 的帮助串是 "(static only)";规格 §3 的原话是"`check`(装前不可信内容)**强制静态**,不接受 `--llm`"。

想让判官看一眼单个目标的人,今天只有一条绕路:

| 绕路 | 后果 |
|---|---|
| **`scan --root <目标>`** | `scan` 的根是"运维自己的环境",所以**自动读 `<root>/.aguardignore`**(`resolveIgnorePath`,`autoBaseline: true`)。目标自带一份列着自己规则 ID 的基线,就能压掉自己的发现、让 `--fail-on` 不响——这正是 `check` 故意拒绝的形状(`TestCheckTarget_TargetSuppliedBaselineIgnored`)。为了用判官,CI 用户得重新打开一个已经关上的洞 |
| **搭一个假的 `.claude` 树,把目标放进 `skills/<name>/`** | baselines 的 adapter 就是这么做的(`baselines/adapter/aguard/stage.go` 的 `Stage`)。分数变成环境聚合分而不是这个 artifact 的分;还要记得 `--inbox off`,否则 CI 机器的 `~/Downloads` 也被扫进来;布局不对的目标(裸 MCP 源码、单个文件)根本放不进去 |

而"装前不可信内容"这个理由,在 `scan` 那边已经不成立了:`scan --llm` 对 `~/Downloads` 里**还没装**的候选逐个走 `checkTarget`,
判官照跑(`cmd/aguard/inbox.go` 的 `checkCandidate`)。同一类内容、同一条代码路径,从 `scan` 进来能判,从 `check` 进来不能。

还有一处规格自己不一致:§3 命令行那行写"强制静态,**忽略** `--llm`",下面一条写"**不接受** `--llm`"。实际行为是后者。

一句话:**最需要判官第二意见的那条命令——审一个别人写的、还没装的东西——恰恰是唯一拿不到它的;而拿到它的唯一办法比不拿更危险。**

## 初步方向

`check` 注册 `--llm` 和 `--fail-on-llm`,语义与 `scan` 完全相同:`--llm` 是同一个显式开关、发同一份脱敏摘录;
`--fail-on-llm` 同样要 `llm.authority: escalate`,没授权是报错拒绝;`--fail-on` 照旧只看确定性发现。
加载时闸门(`aguard hook`)和 `aguard approve` 构造的 `scanOpts` 永不设 `llm`,加一条测试钉住。
规格 §3 / §5.2 / §16 和几处"`check` 恒静态"的文档跟着改。baselines 的 adapter 继续只给 `scan` 传 `--llm`,只改理由。
