---
paths:
  - "internal/score/**"
---
<!-- SPDX-License-Identifier: MIT -->
## 评分(`internal/score`)

```
单 artifact = clamp(100 − Σ_维度 max(严重度惩罚), 0, 100)   # critical 40 · high 25 · medium 12 · low 5
总分        = 各单元分数的平均值,再套木桶封顶: 出现 critical → ≤49 · 出现 high → ≤69
单元        = 一个 artifact;插件和它的子项(Plugin 指回它的 skill/命令/子 agent)是一个单元,发现合在一起算
风险等级    = ≥85 Low · ≥70 Watch · ≥50 Elevated · <50 High
```

同一维度内只取最高的那一次命中,跨维度惩罚相加。**可复现是硬要求**(它要喂给规划中的上链
attestation),因此任何非确定性的东西都不能进这个数。

**插件的子项不能各算一项**(P-044,`family.go`)。子项读的是插件树已经读过的字节,单独平均会稀释插件:实测 `scan` 94 → 98、
`check <插件>` 88 → 98,也就是 `issues/008` 当年放弃拆产物的那个 86 → 97。**父子关系只认 collect 填的 `Plugin` 字段**(经
`score.Families`),不认名字里的 ` (plugin …)` 后缀 —— 那是插件作者写的配置键。`TestApply_PluginAndChildrenAreOneUnit` 钉住两头:
子项不稀释,子项上合格的 LLM 发现照样进 `overall_effective`。报告的"Worst single item"按同一张单元表数(`UnitScores`)。
