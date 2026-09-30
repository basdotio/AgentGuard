---
name: release
description: "阶段 5「发版」的入口,只由人触发。用法:/release x.y.z。不开 proposal:建 p/release-x.y.z,改两处 version、ROADMAP 首段、各 proposal 发布字段,在 complete/ 里留一份发版记录,make verify,直接开 PR。tag、push、npm、GitHub release 由人执行。依据 docs/process.md §1 阶段 5、§2、§5;.claude/rules/npm.md、plugin.md。"
disable-model-invocation: true
---
# /release x.y.z — 攒够了就发,不开 proposal

依据:`docs/process.md` §1(阶段 5)、§2(检查点)、§5(目录即状态);也是 `/propose` 阶段 5 人说"要发版"之后接进来的地方;
`.claude/rules/npm.md`(发布顺序、可重跑)、`.claude/rules/plugin.md`(三处版本号)。输入:$ARGUMENTS 是版本号 x.y.z,不带 v。

**准入**:`git describe --tags --abbrev=0` 之后 `docs/proposals/complete/` 里至少一份「发布」为 `待发`。没有 → 停,说没东西可发。

1. 列出这些 proposal:`complete/` 里「完成」节「发布」为 `待发` 的,对照 `git log <tag>..HEAD --grep 'P-'`。
2. changelog:每个 proposal 取「问题」一节,用后果说一句;**不从提交信息猜**。版本号按行为变化定 minor/patch,记一句为什么。
3. `git fetch`;`git switch -c p/release-x.y.z origin/<主干>`。**只改**:
   `plugin/.claude-plugin/plugin.json` 与 `.claude-plugin/marketplace.json` 的 `version`;`ROADMAP.md`「Shipped since」
   标题范围与首段(changelog 原文);各 proposal「完成」节「发布」填 `vx.y.z`;索引。
4. 在 `complete/NNN-release-x.y.z.md` 留记录(编号照常取):包含哪些 P、为什么这个号、changelog、改了哪里、
   「完成」带证据行(`TestMarketplaceEntryVersionMatchesPlugin`、版本前 → 后、发布字段改了几份)。不走 draft/design。
5. `make verify`。提交 `(P-NNN)`,推分支,开 PR 到 `dev`,描述 = 版本号理由 + changelog + 改动清单,同 `/propose` 步骤 14。
6. 人合入后,**由人**:`git tag vx.y.z`、push tag、按 `npm.md` 顺序发 npm(四个平台包先、launcher 最后、
   每包先 `npm view` 判存在)、再 `gh release create`。这一步 skill 不碰。检查点。
