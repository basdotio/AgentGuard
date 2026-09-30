<!-- SPDX-License-Identifier: MIT -->
# docs/

一棵文档树,目录名即分类。每行只说装什么、写给谁;各文件的分工细节以文件自己的开头为准。

| 位置 | 装什么 | 写给谁 |
|---|---|---|
| [architecture.md](architecture.md) / [.zh-CN](architecture.zh-CN.md) | as-built 地图:流水线、包划分、不变量、当前状态 | 新加入的同事 |
| [rules.md](rules.md) | 规则参考,**由 `make docs` 从代码生成**,CI 卡漂移;规则条数以它头部的计数块为准 | 报告的读者 |
| [install-gate.md](install-gate.md) · [llm-judge.md](llm-judge.md) · [clean-guide.md](clean-guide.md)(前两份有 `.zh-CN` 对子) | 使用指南 | 用户 |
| [spec/](spec/spec.zh-CN.md) | 规格源头;源码注释 `spec §N` 指向它 | 改带不变量代码的人 |
| [corpus-benchmark.zh-CN.md](corpus-benchmark.zh-CN.md) | 语料与 benchmark 的口径:什么算一个正例、按来源带 n、哪些数字不能相加。`baselines/results/` 的读法 | 做语料、读 benchmark 的人 |
| [internals/](internals/) | [clean 技术手册](internals/clean-internals.md)、两份加载行为的实测记录 | 审计或改那段代码的人 |
| [decisions/](decisions/) | [benchmark-review-2026-09-27](decisions/benchmark-review-2026-09-27.zh-CN.md)(三条静态线一条判官线的总评)· [judge-comparison-2026-09-25](decisions/judge-comparison-2026-09-25.zh-CN.md)(判官对照的出处) | 读 benchmark 数字的人 |

不在这棵树里的三份,在仓库根:[CLAUDE.md](../CLAUDE.md)(文档地图与常用命令;**不要**怎么改的各包防护点在 [.claude/rules/](../.claude/rules/),按路径加载)、
[ROADMAP.md](../ROADMAP.md)(做过什么、承认什么限制)、[issues/](../issues/README.md)(确认存在的缺陷与否决记录)。

判据一句话:**这条修完之后,这段文字还有没有价值?** 有 → `issues/`;没有 → 不留档,修法写在改动本身里。
