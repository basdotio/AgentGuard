<!-- SPDX-License-Identifier: MIT -->
# 027 — 发布记录 v0.13.0:反弹 shell(024)、目的地一致性(025)、解码即执行(026)、量测台判官透传与第三列(021、022)

- **来源**:领导 2026-09-29 说"发 v0.13.0"。发版不走 proposal,只在这里留记录
- **包含**:P-021、P-022、P-024、P-025、P-026(`complete/` 里「发布」为待发的全部)。P-023 是规划文档,「发布」为"不随版本发布",不在内
- **分支**:`p/release-0.13.0`

## 为什么是 minor

024 加了 `BD-004`(high)、026 加了 `EXEC-011`(high),`check --fail-on high` 对同一份输入的答案会变(原来放行的反弹 shell、
解码即执行现在拦下);025 把一部分 `EXFIL-001` 从 high 降到 low + advisory,同样改变闸门答案(原来拦下的集成示例现在放行)。
行为变化 = v0.13.0。021、022 是维护者工具(`baselines/`),不影响二进制。没有新 flag、没有删规则,不是 major。

## changelog(从各 proposal 的「问题」一节拼,不从提交信息猜;同一段进 ROADMAP;ROADMAP 里**不放比率**)

见 `ROADMAP.md`「Shipped since v0.1.0」首段 **v0.13.0 (2026-09-29)**。五条要点:反弹 shell 从只认三个 shell 惯用法、只给 low,
变成认"shell 的标准流接到套接字"这个形状并判 high,含 Python/Node/Go 语言原生形态;凭据发往它自己服务的链不再算外泄;
base64 解开直接进 shell 的形状判 high,但不解码任何东西;量测台能带判官跑且在 `run.yaml` 里写明内容出机器;Cisco skill-scanner
成为第三列。另有非 proposal 的三项:两轮判官运行各自一个目录提交进 `results/`(PR #25、#26),四组实验的书面总评(PR #24)。

语料上的数字(只在这里和各 proposal 里;agent-artifact-corpus @ 4f964622,阈值 high,信誉库关):

- P-024:恶意 87 → 98/300(+11,全部真阳性,设计估 +8);良性 147 不变;真机 `BD-004` 0(修掉一个 minified bundle 假阳性后)。
- P-025:良性 147 → 137/3220;恶意 98 不变,0 假阴性(设计阶段量 26 个恶意 `EXFIL-001` 无一被降);真机降级 3 条全部合法。
  **到不了 plan 第 4 步写的"约 3%"**,各 proposal 已用实测更正。
- P-026:恶意 98 → 103/300(+5);良性 137 不变;真机 `EXEC-011` 0。选了"形状提级、不解码",plan 原写的"还原后再扫"没做。
- 合计:恶意 **87 → 103/300**,良性 **147 → 137/3220**,hard negative 4/19 不变,attribution 51/87(59%)→ 65/103(63%)。
- P-022:skill-scanner 2.1.0 strict 档,恶意 27/300(它读了的 143 个 skills 面里 25),良性 76(它读了的 2,480 里),
  897 个 no-verdict 全是 unsupported-input;provisional。
- P-021:无数字,量测台能力;判官数不进 `results/` 的规矩后被 PR #25 改为"单独目录、永不当基线"。

## 改了哪里

| 处 | 前 | 后 |
|---|---|---|
| `plugin/.claude-plugin/plugin.json` `version` | `0.12.0` | `0.13.0` |
| `.claude-plugin/marketplace.json` 插件条目 `version` | `0.12.0` | `0.13.0` |
| `ROADMAP.md`「Shipped since v0.1.0」 | 范围到 v0.12.0 | 范围到 v0.13.0,上面的 changelog 作首段 |
| `complete/021`、`022`、`024`、`025`、`026`「完成」节 `发布:` | 待发 | v0.13.0 |
| 索引:021/022/024/025/026 五行 | PR 待合入;待发 | PR #N 合入;v0.13.0 |
| 索引:023 一行(顺带,过期状态) | PR 待合入;不随版本发布 | PR #27 合入;不随版本发布 |
| 索引:本记录一行 | — | 新增 |
| git tag `v0.13.0` | — | **由人打**,PR 合入后 |

不碰 `.go`、不碰 spec、不碰 npm 包内容(`make npm-dist` 从 tag 派生版本)。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-027 找)
发布:v0.13.0 —— tag、push、npm、GitHub release 由人做,顺序按 .claude/rules/npm.md
证据:TestMarketplaceEntryVersionMatchesPlugin(cmd/aguard/plugin_manifest_test.go)绿;两处 version 0.12.0 → 0.13.0;
      发布字段 待发 → v0.13.0 共 5 份(021、022、024、025、026);make verify 全绿
```
