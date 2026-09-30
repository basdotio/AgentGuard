<!-- SPDX-License-Identifier: MIT -->
# Proposals

每份 proposal 回答"要做什么、做到什么程度算完、明确不做什么"。守则见 [../process.md](../process.md);
新建照 [TEMPLATE.md](TEMPLATE.md)。编号三位递增、全局唯一,建文件时取 `origin/<主干>` 索引里最大号加一。

## 目录即状态(2026-09-20 起)

文件在哪个子目录,就是什么状态;文件里**不写** `状态:` 行,两份真相必漂。状态变更是一次 `git mv`,文件名不变,
`git log --follow` 和 `git log --grep P-NNN` 照常。**draft 和 design 只在 `p/NNN-slug` 分支上**,主干这里只有
`complete/` 和 `rejected/`;在做什么看 `git branch -r --list 'origin/p/*'` 和打开的 PR。

| 目录 | 含义 | 文件里有什么 |
|---|---|---|
| `draft/` | 提出了,人还没说值不值得设计(分支上) | 只有问题、来源、后果、初步方向 |
| `design/` | 人说值得设计。AI 补判据、不做什么、不能说什么、工作项,把未决问题**一次问完**;人答完即接受,接着实现;直到开 PR 都留在这里(分支上) | 六节齐全 |
| `complete/` | 做完了,人说交付;「完成」一节填好,带证据行(`hack/check-proposal` 拦没有的),随 PR 到主干。**发版记录也放这里**(`NNN-release-x.y.z.md`,不走 draft/design) | 六节 + 「完成」;发版记录是包含的 P、changelog、改了哪里、证据行 |
| `rejected/` | 做完或做到一半人说不要,或与另一份是同一件事;同样随 PR 到主干 | 正文写否决理由,或并入哪份 |

"进行中"不是目录:`p/NNN-*` 分支存在就是进行中。

## 索引

| P | 标题 | 来源 | 分支 / 发布 |
|---|---|---|---|
| [001](complete/001-adopt-process.md) | 采用开发流程守则 | 领导要求(2026-09-15) | 例外条款,不建分支 |
| [002](complete/002-merge-doc-dirs.md) | 合并 `doc/` 与 `docs/`,`work-items` 冻结 | P-001 工作项 → 002 | 合入 fc83409 |
| [003](complete/003-split-claude-md.md) | 拆 `CLAUDE.md`,防护点按包进 `.claude/rules/` | P-001 工作项 → 003 | 合入 0444568 |
| [004](complete/004-enforcement.md) | 强制装置:`make verify`、两个 hook、守则三处修订 | P-001 工作项 → 004;002/003 的修订意见 | 合入 927dd4d |
| [005](complete/005-gate-lets-the-evasion-corpus-through.md) | ToB 四个恶意样本全部通过闸门并被记成"已信任" | 新发现(2026-09-16,语料实测) | 合入 634ff6d;v0.10.0;三条跨机器判据未验证,W4 待语料仓 |
| [006](complete/006-process-skills.md) | 四条项目本地命令 `/propose` `/work` `/ship` `/release` | P-001 工作项 → 005(撞号,改为 006) | 合入 78ef115 |
| [007](complete/007-onboarding.md) | 接入:README 指向守则、默认分支改 `dev`、取号先 fetch、lint 安装 | 006 未决 8;接入评估 | 合入 5472bf2;一条判据合入后撤回 |
| [008](complete/008-markdown-report.md) | 扫描结果贴不进 PR 评论和 issue,只能手抄;加 `--md` | 新发现(2026-09-20,对话提出) | PR #7 合入;v0.10.0 |
| [009](complete/009-release-0.10.0.md) | 发布记录 v0.10.0:005 + 008 | 领导 2026-09-20 | PR 待合入;tag 由人打 |
| [010](complete/010-env-dump-credential-leg.md) | `env \| curl` 把整个环境发出去,aguard 一条发现都不给 | 新发现(2026-09-21,语料首轮漏检) | [PR #9](https://github.com/basdotio/agent-guard/pull/9) 合入;v0.11.0 |
| [011](complete/011-corpus-runner.md) | 语料仓建好了,aguard 却没有 runner 跑它,误报率仍无分母 | 新发现(2026-09-21,对话提出) | [PR #10](https://github.com/basdotio/agent-guard/pull/10) 合入;v0.11.0 |
| [012](complete/012-instruction-url-literal.md) | SKILL.md 里一个 URL 字面量就算"出网",配置示例被判成外泄链 | plan §3.1 第 1 步;W-026 语料实测版(2026-09-21) | [PR #11](https://github.com/basdotio/agent-guard/pull/11) 合入;v0.11.0 |
| [013](complete/013-config-url-literal.md) | `.mcp.json` 里给自己服务器的鉴权头被判成外泄链 | P-012 实现阶段顺带发现(2026-09-21) | [PR #12](https://github.com/basdotio/agent-guard/pull/12) 合入;v0.11.0 |
| [014](complete/014-release-0.11.0.md) | 发布记录 v0.11.0:010 + 011 + 012 + 013 | 领导 2026-09-21 | [PR #13](https://github.com/basdotio/agent-guard/pull/13) 待合入;tag 由人打 |
| [015](complete/015-baseline-runners-have-no-home.md) | 对照表只能靠手写的规则数:第三方扫描器在同一份语料上跑不起来 | 新发现(2026-09-21,对话提出) | [PR #14](https://github.com/basdotio/agent-guard/pull/14) 合入;v0.12.0;**部分交付**:W6/W11(Heeler)未做,卡在账号与 ToS |
| [016](complete/016-config-surface-shapes.md) | 配置面上四种朴素攻击形状零发现:MCP env 注入、改写权限文件、hook 自动放行、API 端点被改 | plan §3.1 第 2 步第一组(2026-09-23) | [PR #16](https://github.com/basdotio/agent-guard/pull/16) 合入;v0.12.0 |
| [017](complete/017-ccaudit-adapter.md) | 唯一和我们打平的竞品,只被测过一次:一台机器、一个样本 | 新发现(2026-09-23,对话提出);audit-log 条目 3/4 | [PR #17](https://github.com/basdotio/agent-guard/pull/17) 合入;v0.12.0 |
| [019](complete/019-llm-009-advisory-only.md) | 判官把"MCP 包没钉版本"当高危送进闸门,真实配置每百个多拦一个 | 新发现(2026-09-25,判官子集实测) | [PR #20](https://github.com/basdotio/agent-guard/pull/20) 合入;v0.12.0 |
| [020](complete/020-release-0.12.0.md) | 发布记录 v0.12.0:015 + 016 + 017 + 019 | 领导 2026-09-26 | [PR #21](https://github.com/basdotio/agent-guard/pull/21) 合入;tag 由人打 |
| [021](complete/021-bench-judge-passthrough.md) | 报告里的判官数字靠一份没进仓的量测台改动跑出来,别人复现不了 | 新发现(2026-09-27,对话提出) | [PR #22](https://github.com/basdotio/agent-guard/pull/22) 合入;v0.13.0 |
| [022](complete/022-cisco-skill-scanner.md) | 最像我们的对照物,却在语料里批着自己出的卷子 | 新发现(2026-09-24,对话提出);competitor-research §3.4;原 P-019,撞上游同号,后推的改号 | [PR #23](https://github.com/basdotio/agent-guard/pull/23) 合入;v0.13.0;**strict 档只读 skills 面**,MCP 行不可测 |
| [023](complete/023-plan-restated-by-diff.md) | 照着 plan §3.1 开工会撞上一道按定义够不到的门槛、两处指向别人的编号,和少了最便宜一关的判官步 | 对话(2026-09-28);benchmark-review-2026-09-27 | [PR #27](https://github.com/basdotio/agent-guard/pull/27) 合入;不随版本发布 |
| [024](complete/024-reverse-shell-passes-gate.md) | 教科书级反弹 shell 通过闸门:唯一认它的规则只认 shell 惯用法,而且只给 low | W-027(2026-09-16 实测);plan §3.1 第 2 步,执行序 1 | [PR #28](https://github.com/basdotio/agent-guard/pull/28) 合入;v0.13.0 |
| [025](complete/025-exfil-destination-consistency.md) | 凭据发往自家服务域名的链，和发给攻击者的链，字面一模一样 | plan §3.1 第 4 步，执行序 2；issues/016 | [PR #29](https://github.com/basdotio/agent-guard/pull/29) 合入;v0.13.0 |
| [026](complete/026-decode-then-execute.md) | base64 解开就执行的载荷，只得一条 medium，通过闸门 | plan §3.1 第 3 步，执行序 3 | [PR #30](https://github.com/basdotio/agent-guard/pull/30) 合入;v0.13.0 |
| [027](complete/027-release-0.13.0.md) | 发布记录 v0.13.0:021 + 022 + 024 + 025 + 026 | 领导 2026-09-29 | [PR #31](https://github.com/basdotio/agent-guard/pull/31) 合入;tag 在分支提交 `9a48baa`,不在 dev 历史上(见 032) |
| [028](complete/028-tool-description-secrets.md) | 工具说明里提到"密码""settings.json"就判 high:353 个真实工具目录里拦错 20 个,两种真投毒却放行 | plan §3.1 第 1 步剩余,执行序 4 | [PR #32](https://github.com/basdotio/agent-guard/pull/32) 合入;v0.14.0 |
| [029](complete/029-rm-rf-scoped-paths.md) | 清一个缓存目录也判"删整个家目录":FS-003 把起头是 `/` 或 `~` 的路径都当成根 | plan §3.1 第 1 步剩余;P-028 的 FS 测量 | [PR #33](https://github.com/basdotio/agent-guard/pull/33) 合入;v0.14.0 |
| [030](complete/030-remote-instruction-and-beacon.md) | 把主机名塞进外发 URL、从远程取 instructions.md 照做:两种形状零发现,通过闸门 | plan §3.1 第 2 步剩余,执行序 5 | [PR #34](https://github.com/basdotio/agent-guard/pull/34) 合入;v0.14.0 |
| [031](rejected/031-defensive-prose-hard-negatives.md) | 三份"教你防御注入"的土耳其语红队文档被判成注入:静态分不开"引用来防御"与"实施攻击" | plan §3.1 第 1 步剩余,执行序 6;issue 011 | [PR #35](https://github.com/basdotio/agent-guard/pull/35) 合入;否决;归判官 |
| [032](complete/032-release-0.14.0.md) | 发布记录 v0.14.0:028 + 029 + 030 | 领导 2026-09-29 | [PR #36](https://github.com/basdotio/agent-guard/pull/36) 合入;tag 在 dev 的 `cce362f` |
| [034](complete/034-scorer-out-of-scope.md) | 语料标签里有"该工具不检测"这个字段,评分器却从不读它:两道召回门槛只能靠估算 | P-023 已决 1、2 | [PR #37](https://github.com/basdotio/agent-guard/pull/37) 合入;v0.15.0;语料仓 PR #1 已合入 |
| [035](complete/035-majority-severity.md) | 把每一票的严重度记下来:三票只留第一票,判定为什么变了事后读不出来 | 方差实验(2026-09-29,PR #39) | [PR #40](https://github.com/basdotio/agent-guard/pull/40) 合入;v0.15.0;多数票取法实测 0 / 224 未采用 |
| [036](complete/036-release-0.15.0.md) | 发布记录 v0.15.0:034 + 035 | 领导 2026-09-29 | PR 待合入;tag 由人打 |
| [037](complete/037-results-paths-relative.md) | 已提交的测量结果带着同事的家目录和本机临时路径,一份账本里 1,669 处 | 开源前清理扫描(2026-09-30),同事要求保留 results | PR 待合入;待发 |
| [038](complete/038-dead-code.md) | 四处没人调用的代码在仓里占位:预留接口、兼容别名、只有测试用的导出、一次性工具 | 开源前清理扫描(2026-09-30) | PR 待合入;待发 |
| [039](complete/039-results-trim.md) | results 里六个只被完成提案引用的中间 run、6.2 MB 引用恶意原文的 raw、一条从未跑过的工具登记 | 开源前清理扫描(2026-09-30),决定三 | PR 待合入;待发 |
| [040](complete/040-comments-internal-refs.md) | 源码注释里 65 处 P-NNN、46 处 W-NNN、12 处 issues 路径和 3 处进了用户字符串的 spec 引用,新仓没有它们指向的东西 | 开源前清理扫描(2026-09-30) | PR 待合入;待发 |
| [041](complete/041-internal-docs-out.md) | 两份内部证据文档、一份语料口径放错了目录、issues 里的 sha 和里程碑编号,在没有历史的新仓里都是死引用 | 开源前清理扫描(2026-09-30),决定二的第一步 | PR 待合入;待发 |

## 回溯

- 从代码到 proposal:`git blame` → 提交信息末尾 `(P-NNN)` → 本目录(四个子目录里找编号)
- 从 proposal 到代码和版本:`git log --grep 'P-NNN'`;`git tag --contains <合入提交>`;
  文末「完成」一节直接记着合入提交和发布版本
