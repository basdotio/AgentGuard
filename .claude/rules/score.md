---
paths:
  - "internal/score/**"
---
<!-- SPDX-License-Identifier: MIT -->
## 评分(`internal/score`)

```
单 artifact = clamp(100 − Σ_维度 max(严重度惩罚), 0, 100)   # critical 40 · high 25 · medium 12 · low 5
总分        = 各 artifact 分数的平均值,再套木桶封顶: 出现 critical → ≤49 · 出现 high → ≤69
风险等级    = ≥85 Low · ≥70 Watch · ≥50 Elevated · <50 High
```

同一维度内只取最高的那一次命中,跨维度惩罚相加。**可复现是硬要求**(它要喂给规划中的上链
attestation),因此任何非确定性的东西都不能进这个数。

