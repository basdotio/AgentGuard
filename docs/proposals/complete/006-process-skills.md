<!-- SPDX-License-Identifier: MIT -->
# 006 — 四条项目本地命令:`/propose` `/work` `/ship` `/release`,每条一屏,内容是"读守则第 N 节照做"

- **来源**:P-001 工作项 → 005(编号在 2026-09-16 被另一会话的 005 先占,本条改为 006);领导 2026-09-16 定"先在本仓库内规范,不做独立 skill"
- **依赖**:P-004(已完成)
- **分支**:`p/006-process-skills`

## 问题

守则、模板、闸门、hook 都有了,但**入口靠人记**:开工要记得先 fetch、建 `p/` 分支、测试先红;交付要记得 rebase、
攒证据、按判据逐条对照;发版要记得只碰两个 JSON。002 到 004 这三轮每一步都是我在对话里手动展开的,换一个会话、
换一个人,展开的顺序和遗漏各不相同。守则 §1 的四个 AI 阶段,每个该有一个固定入口。

Claude Code 项目级 skill 就是这个入口:`.claude/skills/<name>/SKILL.md`,敲 `/<name> 参数` 触发,
`disable-model-invocation: true` 让它**只由人触发**,模型不会因为对话里出现"发版"两个字就自己跑去发版
(来源:code.claude.com/docs/en/skills,2026-09-16 核实;正文建议 500 行内,`$ARGUMENTS` 取参数)。

## 目标

四个文件,各不超过 60 行正文。**它们不重复守则,只做三件事**:说明这一阶段的准入条件、按顺序列出动作、指明
`docs/process.md` 的哪一节是依据。守则改了,skill 不用改。

| 命令 | 参数 | 准入 | 动作(每条对应 002–004 实际做过的一步) | 出口 |
|---|---|---|---|---|
| `/propose <一句话问题 \| issues/NNN \| 修 P-NNN>` | 问题 | 无 | 查 `issues/` 与 `proposals/` 有没有记过或重复 → 取下一个编号 → 按 TEMPLATE 写草稿(来源、判据、不做什么、不能说什么、工作项)→ **不清楚的一次问完**写进未决问题 → 索引加 `草案` 行 → **不提交** | 草稿 + 要人答的问题。人接受后:记录决定、状态 `已接受`、索引同步、提交到主干 |
| `/work NNN` | 编号 | 状态 = `已接受` | `git fetch` → 从主干建 `p/NNN-slug` → 状态 `进行中` → W1 **先写测试跑红** → 逐 W 实现,一 W 一提交 `(P-NNN)` → 动了不变量同步 `spec`;加/改规则跑 `make docs` → 疑问攒进未决问题,不中断(越界除外)→ `make verify` → collect/detect 有改动跑真机扫描 | 分支就位,闸门绿 |
| `/ship NNN` | 编号 | 分支存在,闸门绿 | `git fetch` + rebase 到主干,冲突自己解、写进未决问题 → `make verify` → 判据逐条对照证据;「不做什么」逐条给未改动的证明(`git diff --stat 主干 -- <路径>`)→ 把 AI 不确定的点写进未决问题并提交 → 输出 review 包 + 合入命令(`switch` 主干、`merge --ff-only`、`push`、删分支) | review 包。人合入后再敲一次 `/ship NNN`:填「完成」(sha、证据行)、索引 `已完成`、提交到主干(例外第四种) |
| `/release x.y.z` | 版本号 | 自上个 tag 起 ≥ 1 个 `已完成` proposal | 列出这些 proposal → 用各自「问题」一节拼 changelog 草稿 → 开 release proposal(`NNN-release-x.y.z.md`,`草案`)→ 人接受后建 `p/release-x.y.z` → **只改** `plugin/.claude-plugin/plugin.json`、`.claude-plugin/marketplace.json` 的 `version` 和 ROADMAP「已发布」一节 → `make verify` → review 包 | 人合入、本地 `git tag vx.y.z`、push、按 `.claude/rules/npm.md` 顺序发 npm 与 release |

发版的三处版本号里 git tag 那一处由人打,skill 不碰;changelog 进 ROADMAP 现有的「Shipped since」一节,
不新建 `CHANGELOG.md`(现状如此,见未决问题 1)。

## 完成的判据

- [ ] 四个 `SKILL.md` 存在;frontmatter 有 `name`(= 目录名)、`description`(≤ 1024 字符,与 `plugin/` 里桌面版限制同一个数)、
      `disable-model-invocation: true`;正文 ≤ 60 行;正文引用 `docs/process.md` 的具体节号
- [ ] 新增 `TestProcessSkillsStayThin` 钉住上面每一条;**反向断言**:61 行正文、缺 `disable-model-invocation`、
      `name` 与目录名不符,各被抓住
- [ ] **自扫**:`make verify` 的自扫那行扩成对 `plugin/` 和每个 `.claude/skills/*` 都跑 `aguard check --fail-on low`,
      exit 0 —— 这个仓库教人审 skill,自己带的 skill 先过自己的闸门
- [ ] `TestDocsRelativeLinksResolve`、`TestClaudeRulesAreScopedToExistingPaths` 仍绿;`plugin/` 零改动
- [ ] **人工核对(检查点 2)**:新会话里 `/propose 随便一句问题`,产出的草稿字段与 TEMPLATE 一致且没有提交;
      `/work 006` 在状态不是 `已接受` 时拒绝开工

## 不做什么

- 不做 plugin、不进 marketplace、不注册 hook 或 MCP(和 `plugin.md` 规则同一条:一个教人提防的工具不顺手给人装东西)
- 不在 skill 里复制守则正文;守则是唯一版本
- 不动 `plugin/` 下三个已发布的 skill
- 不给 `dev` 之外的分支模型加任何自动触发;四条全部 `disable-model-invocation: true`
- 不新建 `CHANGELOG.md`

## 不能说什么

不适用于报告文案。对本条:skill 的 `description` 只写它做什么、怎么触发,**不写"保证流程合规"**。它是入口,
合规靠 hook 和 `make verify`,靠人看 review 包。

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | `TestProcessSkillsStayThin`(含反向断言),对当前树:目录不存在应直接通过 | `test(skills): the four process skills stay thin, user-invoked, and point at process.md …` |
| 2 | 四个 `SKILL.md` | `skills: /propose /work /ship /release, each a one-screen entry into docs/process.md` |
| 3 | `make verify` 自扫扩到 `.claude/skills/*`;`CLAUDE.md` 常用命令加四条命令一行;`docs/README.md`、`process.md` §1 提到入口 | `make: verify self-scans the process skills; docs name the four entry points` |

## 未决问题

1. **changelog 住哪。** 现状是 ROADMAP「Shipped since」一节,v0.9.0 那段就在那里。建议维持,`/release` 往那一节
   追加;要独立 `CHANGELOG.md` 另开 proposal。**已决(2026-09-16):维持 ROADMAP。**
2. **`/ship` 敲两次**(合入前出包、合入后收尾)还是拆成 `/ship` 和 `/close`。建议一条命令按状态分支:
   分支还在 → 出包;分支已合入 → 收尾。少记一个名字。**已决(2026-09-16):一条命令分岔。**
3. **release proposal 要不要也编号进 `docs/proposals/`。** 建议要:它走同一套状态和 hook,`git log --grep`
   一样能找到;文件名带 `release-` 前缀区分。**已决(2026-09-16):编号进目录。**
4. **W1 的提交被 amend 过一次。** 第一版反向断言的 fixture 行数算错(60 行填充 + 2 行引用 = 63,不是 61),
   我的命令链在测试红的情况下照样提交了。修好后 `--amend`,分支未推送。检查器本身没错,错的是 fixture 的算术。
5. skill 正文合计 101 行,单个最长 33 行;description 全部中文,和 `plugin/` 里三个英文 skill 不同——那三个面向所有用户,
   这四个只面向本仓库的开发者。
6. `make verify` 的自扫现在对 `plugin/` 和四个 skill 各跑一次 `aguard check --fail-on low`,全部 exit 0。
7. 判据里"新会话 `/propose` 产出草稿且不提交"、"`/work 005` 在状态不是已接受时拒绝"两条是人工核对,我没法在本会话里验。
8. **编号撞车。** 本条草稿取号时索引最大是 004,取了 005;另一会话同时按守则开了它的 005(闸门放过恶意语料)并先推到远端。
   本条改为 006:文件、分支、索引、001 的工作项表,以及所有未推送提交信息里的 `P-005`(用 `git rebase -x` 逐个 amend)。
   守则的下一条修订意见:**取号在建分支前 `git fetch`,以 `origin/<主干>` 的索引为准**;两个会话并行时仍可能撞,撞了后推的改号。
9. rebase 到 origin/dev 时索引冲突两次,用"同编号以正在重放的提交为准、不同编号取并集"解;第二次解析把对方的 005 行
   盖掉了(我旧编号也是 005),事后发现补回,单独一个提交。合入前请看一眼索引六行是否齐全。
10. 本分支还带着两个原本直接进主干、但没来得及推送的提交(004 完成、001 更正),rebase 后它们在分支上;合入命令因此
   要先把本地 `dev` 对齐 `origin/dev` 再快进,和 002 那次一样。

## 完成

```
合入:78ef115(2026-09-16,ff 合入 dev;分支已删)
发布:不随版本发布
证据:TestProcessSkillsStayThin + TestProcessSkillProblemsAreCaught(cmd/aguard/process_skills_test.go);
      四个 SKILL.md 合计 101 行、最长 33 行;make verify 对 plugin/ 与 .claude/skills/* 各跑 aguard check --fail-on low 全部 exit 0;
      pre-commit 在提交 skills 时实跑了同一闸门;人工核对两条(/propose 不提交、/work 拒绝非已接受)待首次使用时验
```
