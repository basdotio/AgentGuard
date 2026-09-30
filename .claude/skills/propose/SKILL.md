---
name: propose
description: "从一句话问题一路走到 PR 的入口,只由人触发。用法:/propose <一句话问题 | issues/NNN | 修 P-NNN>。建 p/ 分支写 draft → 人说值得设计 → 补 design 一次问完 → 人答完直接实现 → 做完问交付还是废弃 → git mv 到 complete/ 或 rejected/,开 PR 到 dev → 合入后问要不要发版。依据 docs/process.md §1–§6。"
disable-model-invocation: true
---
# /propose — 一个问题,一条分支,一个 PR

依据:`docs/process.md` §1(五个阶段)、§2(人出现的地方)、§3(分支与提交)、§4(闸门)、§5(目录即状态)、§6(证据行)。
模板:`docs/proposals/TEMPLATE.md`。输入:$ARGUMENTS —— 一句话问题、`issues/NNN`,或"修 P-NNN"。
全程在同一个分支上,**停下问人的只有四处**(值得设计吗、答问题、交付还是废弃、要不要发版);其余疑问攒进「未决问题」不打断,越过「不做什么」除外。

## 1. draft

1. 查重:`issues/`、`docs/proposals/*/`、`git branch -r --list 'origin/p/*'`。`design/` 或分支里已有同一件事 → 停,说"和 P-NNN 是同一件事"。
2. 取号:`git fetch`;`origin/<主干>` 索引最大号 + 1(撞了后推的改号)。`git switch -c p/NNN-slug origin/<主干>`。
3. 只写 draft 段(来源、依赖、问题、初步方向)到 `draft/NNN-slug.md`,**没有** `状态:` 行;索引加一行。提交 `(P-NNN)`。
4. **停,问:值得设计吗?** 不值得 → 删分支,源自 issue 的在 issue 里记一句。

## 2. design

5. `git mv` 到 `design/`,索引链接跟着改。读够相关代码、`docs/spec/spec.zh-CN.md` 对应节、相关 issue。
6. 补判据(代码类至少两条,一条反向断言)、不做什么、不能说什么、工作项(一 W 一提交,W1 是测试)、
   未决问题(**一次问完**,每条带建议答案)。提交。
7. **停,交出问题清单。** 人答完写"**已决(日期)**:…";全部已决 = 接受。不合主干,接着做。

## 3. 实现

8. W1 先写测试跑红,红的原因要和判据对得上;再逐 W 实现,一 W 一提交 `<scope>: <用后果说> (P-NNN)`。
9. 动了不变量或数据模型 → 同步 spec;加/改规则 → `make docs`;搬了文件 → 跑链接测试;白名单外不得不改的,改并披露。
10. `make verify` 绿;`internal/collect`、`internal/detect` 有改动 → `./bin/aguard scan --root ~/.claude` 真机,头部贴进未决问题。
11. **停,问:交付还是废弃?** 附判据逐条对照证据、「不做什么」逐条的 `git diff --stat origin/<主干> -- <路径>`。

## 4. PR

12. 交付:填「完成」(合入写 PR 不写 sha;证据行),`git mv design/ → complete/`,索引。废弃:`git mv` 到 `rejected/`,正文写理由。
13. `git fetch`,`git rebase origin/<主干>`,冲突自己解、写进未决问题;`make verify` 重跑。`git push -u origin p/NNN-slug`。
14. review 包写成 `<scratch>/pr-NNN.md`:判据对照表、不做什么的证明、AI 不确定的点。标题 `P-NNN: <标题>`。
    `gh` 在:`gh pr create --base dev --head p/NNN-slug --title … --body-file …`;不在:给
    `https://github.com/basdotio/agent-guard/compare/dev...p/NNN-slug?expand=1&title=<url-encoded>`,描述让人粘。
    **base 必须是 `dev`。不替人合入**:人在页面上 Rebase and merge。

## 5. 合入之后

15. 下一轮开头先 `git fetch`,用 `git log origin/dev --oneline --grep 'P-NNN'` 看合没合。没合 → 问一句"PR 合了吗",等;
    合了 → `git switch dev && git pull --ff-only`,删本地分支,**停,问:要不要发版?**
16. 要 → 按 `/release` 走,`complete/` 里所有「发布」为 `待发` 的都进这一版;不要 → 说一句"P-NNN 合入,待发",结束。
