<!-- SPDX-License-Identifier: MIT -->
# 021 — 只在静态报到 medium 以上时才问判官:实测否决,不要再论证

- **类别**:功能范围(已否决)
- **严重程度**:—
- **状态**:**不规划**。前提经已提交的判官运行实测不成立,2026-09-28(P-023)。

> **这条 issue 的用途是防止重新发明。** 判官贵:819 个样本 1,923 次调用、14.5 小时,全量 3,539 个约两天半。
> "静态已经觉得可疑的才送去判官"听起来是最自然的省钱办法。如果你又想到它,**先跑 §3 的脚本,再决定要不要动手。**

## 1. 出发点

`--llm` 会把每个被判的制品的脱敏摘录发给模型。成本和出机器的内容都与被判的样本数成正比。
静态规则已经给每个样本打了严重度,拿它当筛子,只把 medium 以上的样本交给判官,调用量应该能砍掉一大块。

## 2. 为什么不成立

判官的价值恰好集中在**静态什么都没看见**的样本上。在判官能碰到的 173 个恶意样本里
(去掉走 `check` 的 127 个 MCP 服务器源码,判官不在那条路上),静态放行 100 个,判官抓回 62 个:

| 判官抓回的 62 个,静态最高严重度 | 个数 |
|---|---|
| 零发现 | 43 |
| low | 5 |
| medium | 14 |

按 medium 分流:

| | |
|---|---|
| 判官增益保住 | **14 / 62** |
| 判官增益丢掉 | **48 / 62(77%)** |
| 省下的调用 | 1,215 / 1,923(63%) |

省下六成多的钱,丢掉近八成的增益。静态零发现的那 43 个按来源:skillsgoat 规避靶场 20、skillcraft 复刻 7、
nvidia 语义改写 4、datadog 真实恶意 3、其余 9 个散在 7 个来源——逐行规则一条都没响的地方,正是判官存在的理由。
按 low 分流也救不回来:low 只多 5 个。

## 3. 复现

不需要 API key,不重跑:读的是已提交的判官运行
[`baselines/results/aguard/2026-09-25-llm-glm-5.3-flash/`](../baselines/results/aguard/2026-09-25-llm-glm-5.3-flash/)
(glm-5.3-flash,`samples: 1`,819 子集 = 300 恶意 + 19 hard negative + 500 分层良性,2026-09-25)。
"判官抓回"用 LLM-009 只提示后的口径(P-019):升级规则里去掉 LLM-009 之后还有别的。在仓库根目录:

```python
import json, collections
D = "baselines/results/aguard/2026-09-25-llm-glm-5.3-flash/"
SEV = {"critical": 4, "high": 3, "medium": 2, "low": 1, "info": 0}
def static_max(sample):
    d = json.load(open(D + "raw/" + sample + ".json")); m = -1
    for a in d.get("artifacts") or []:
        for f in a.get("findings") or []:
            if f.get("source") != "llm":
                m = max(m, SEV.get(str(f.get("severity")).lower(), -1))
    return m
rows = [json.loads(l) for l in open(D + "judge.jsonl")]
reach = [r for r in rows if r["class"] == "malicious" and not r["sample"].startswith("mal-mcp-ci")]
missed = [r for r in reach if not r["static"]]
gain = [r for r in missed if r["judge"] and set(r["escalated_rules"]) - {"LLM-009"}]
kept = [r for r in gain if static_max(r["sample"]) >= 2]
calls = sum(r["judge_calls"] for r in rows)
saved = sum(r["judge_calls"] for r in rows if static_max(r["sample"]) < 2)
print(f"static misses {len(missed)} · judge adds {len(gain)} · triage keeps {len(kept)} · loses {len(gain)-len(kept)}")
print(f"calls {calls} · saved by triage {saved} ({saved/calls:.0%})")
```

实测输出(2026-09-28):

```
static misses 100 · judge adds 62 · triage keeps 14 · loses 48
calls 1923 · saved by triage 1215 (63%)
```

## 4. 什么时候值得重看

- 换了模型,或 `samples` 从 1 改成 3 之后,判官增益的构成可能变;用新运行的目录重跑 §3。
- 静态规则大改之后(例如编码还原落地),"静态零发现"那一格会变小;同样重跑。

在那之前,判官的成本要从别处降:付费档的并发、更快的模型、每个样本的趟数(现在平均 2.3 次)。
