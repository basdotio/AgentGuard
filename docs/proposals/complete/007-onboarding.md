<!-- SPDX-License-Identifier: MIT -->
# 007 — 接入:README 指向守则、远端默认分支改 `dev`、守则补"取号先 fetch"和工具安装

- **来源**:006 未决问题 8(撞号);2026-09-16 接入评估的五处缺口;领导定"继续在 `dev`"
- **依赖**:P-006(已完成)
- **分支**:`p/007-onboarding`

## 问题

流程在仓库里是齐的,但一个新人 clone 下来进不了它:

1. **公开 README 一个字没提流程。** 没有 CONTRIBUTING,README 不指向 `docs/process.md`。外人不知道有守则。
2. **远端默认分支是 `main`,落后 `dev` 203 个提交。** 新人默认 clone 到 main,看到的是三个月前的仓库,
   `make verify`、hook、skill 全都不在。领导已定主干继续是 `dev`,那远端就该指 `dev`。
3. **取号会撞。** 006 与另一会话的 005 同日撞号,守则取号规则只写了"索引最大号 + 1",没写以哪份索引为准。
4. **`make verify` 依赖 golangci-lint,守则没写怎么装。** 新人第一次跑在 lint 那步失败,错误是 "command not found",
   不指向任何文档。
5. hook 是本机装置,不跑 `make hooks` 的人不受约束。**本条不解决**:直推 `dev` 的模式下 CI 替不了它;将来走 PR 时另开
   proposal。写进守则的"不能说什么"即可。

## 目标

| 处 | 改什么 |
|---|---|
| `README.md` / `README.zh-CN.md` | 末尾加一节 `## Contributing` / `## 参与开发`,五句话:主干是 `dev`;clone 后 `make hooks`;守则在 `docs/process.md`(中文);入口 `/propose`;`main` 不是主干。双语对子,英文那份说明守则是中文 |
| 远端仓库设置 | 默认分支 `main` → `dev`。**由人在 GitHub 仓库设置里改**(需要 admin),或 `gh repo edit --default-branch dev`。`main` 本身不动、不删、不合 |
| `docs/process.md` | 名词表 `main` 一句:不是主干、落后、不从它开工;§1 阶段 1 取号:**先 `git fetch`,以 `origin/<主干>` 索引最大号 + 1;撞了后推的改号**;§4 加一行工具安装(`brew install golangci-lint` 或 `go install …@v1.x`,版本与 CI 一致) |
| `.claude/skills/propose/SKILL.md` | 第 2 步取号改为 fetch 后按远端索引 |
| `Makefile` `verify` | lint 之前查 `command -v golangci-lint`,缺了打印一句指向 `process.md` §4 再退 1,而不是 "command not found" |

## 完成的判据

- [ ] 两份 README 有新节,里面的相对链接被 `TestDocsRelativeLinksResolve` 覆盖并绿
- [ ] `process.md` 三处改动在;`TestProcessSkillsStayThin` 仍绿(`propose` 仍 ≤ 60 行)
- [ ] `PATH` 里没有 golangci-lint 时 `make verify` 在 lint 步之前退出并打印含 `process.md` 的一句
      (`PATH=/usr/bin:/bin make verify` 实测,写进「完成」);有它时行为不变
- [x] ~~远端默认分支显示为 `dev`~~ **撤回(2026-09-16)**:领导定 `main` 保持默认分支,开发在 `dev`,将来一次性合入。
      守则与 README 改为"clone 后 `git switch dev`"
- [ ] `plugin/` 零改动

本条不写新测试:改的是文档和一行 Makefile,已有的链接测试和 skill 测试是结构检查;`verify` 的工具检查用负向实测。

## 不做什么

- 不动 `main`:不合、不删、不追平。它只是不再是默认分支
- 不建独立 `CONTRIBUTING.md`;README 一节够,两处会漂
- 不改 CI、不把 hook 检查搬进 CI(见问题 5)
- 不翻译守则和 proposal 成英文

## 不能说什么

README 那节**不写"提交受 hook 保护"**。hook 只在跑过 `make hooks` 的机器上存在;要写就写"跑 `make hooks` 装上检查"。
也不写"流程保证质量",流程只保证每次状态变更留了可核对的证据。

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | `process.md` 三处 + `propose` skill 取号步骤 | `docs(process): number from the remote index after a fetch; main is not the trunk; how to install the lint …` |
| 2 | 两份 README 的接入节 | `docs(readme): a contributing section that points at the process …` |
| 3 | `Makefile` verify 的工具检查 | `make: verify says where to get golangci-lint instead of "command not found"` |
| 人 | GitHub 默认分支改 `dev`,然后 AI 复核 | (不是提交) |

## 未决问题

1. **谁有 admin 改默认分支。** 建议合入前就改,这样新人 clone 到的第一份就是有守则的。**已决(2026-09-16):合入前由领导改。**
2. **README 接入节放末尾还是放「安装」后面。** 建议末尾:README 的读者九成是用户不是贡献者。**已决(2026-09-16):末尾。**
3. **golangci-lint 版本。** CI(`.github/workflows/ci.yml`)钉的是 **v2.13.2**,本机装的也是它;守则写这个号,不写 `@latest`。
   CI 注释记着 v1→v2 那次配置格式不兼容的事,升级要和配置一起动。这条不需要人答,写在这里是留底。
4. 负向实测的做法:临时目录里只放 go、git、make 等的软链,`PATH` 指向它加系统目录,`make verify` 在 vet、test 之后、
   lint 之前退出,打印那一句,日志里没有 `golangci-lint run`。
5. 本条没有新测试,理由写在判据下面;三个提交都穿过了装好的 hook,pre-commit 对改动过的 `propose` skill 又跑了一次 aguard。
6. **默认分支改 `dev` 是本条唯一不在 git 里的动作**,由你在 GitHub 仓库设置里做;做完我用
   `git remote show origin | grep 'HEAD branch'` 复核并写进「完成」。README 那节现在就写了"`main` 不是主干",
   在改之前它比现实早一步。

## 完成

```
合入:5472bf2(2026-09-16,ff 合入 dev;分支已删)
发布:不随版本发布
证据:README 两节,链接测试绿;process.md 三处、propose skill 取号步骤,skill 测试绿;
      make verify 全过;PATH 无 golangci-lint 时 exit 2 并打印指向 process.md §4 的一句,日志无 "golangci-lint run"
撤回一条:默认分支改 dev 的判据在合入后被领导否决(main 保持默认分支,开发在 dev,将来一次性合入),
      守则 main 一行与两份 README 改为 "clone 后 git switch dev",随本次完成提交一并进主干。
      守则的两条修订意见留底:六个状态里没有"已合入、等一件 git 之外的事";判据在合入后被改变时怎么记(本条用了删除线加撤回注记)。
```
