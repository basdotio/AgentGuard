<!-- SPDX-License-Identifier: MIT -->
# 001 — 采用开发流程守则:人只提问题、拍板、合入、发布,其余由 AI 按守则执行

- **来源**:领导要求(2026-09-15):doc 结构重设计;从 proposal 到打 tag 的提交与版本守则
- **依赖**:无
- **分支**:本条不建分支 —— 它只增加四个文档文件,属于 `process.md` §3 的例外(不碰 `.go`、不碰 `spec`、
  不改任何既有状态字段)。这是例外条款唯一一次用在流程自身上;之后的 002–005 都走分支

## 问题

功能到 v0.9.0 暂停,仓库有三处结构性欠账:

1. **流程只存在于习惯里。** W 编号、`release vX` 提交、issues 状态集、五条硬约束、交回检查单,这些都在跑,
   但散在 `work-items` §0、`issues/README` 维护约定、`CLAUDE.md` npm 一节三处,没有一份"从提出到打 tag"
   的连贯说明。后果:`issues/` 有两次状态停在旧值;v0.8.5 打了 tag 却没有 release,插件叫用户升级但升不上去(W-015)。
   > **更正(2026-09-16)**:本句第一版写的是"`release v0.9.0` 提交里混进了非发版改动"。经查 `da16366` 只改了两处版本号,
   > 那个说法不成立,是我凭印象写的;换成 W-015 那件有记录的事。写下即冻结的是审计日志,proposal 允许更正,但要留痕。
2. **`doc/` 与 `docs/` 并存**(中文规划 / 双语面向用户),分类靠 `CLAUDE.md` 里一张表维护。
3. **`CLAUDE.md` 90 KB 每次会话全量加载**,内容是按包划分的防护点,却没按包拆。

本条只解决第 1 项的"有没有守则";第 2、3 项和强制装置作为后续 proposal 列在「工作项」里,
按守则自己走一遍 —— 流程能不能跑,拿它自己试最快。

## 完成的判据

- [ ] `docs/process.md` 存在,一页纸内(< 8 KB),含五个阶段、三个检查点、分支与提交约定、
      状态封闭集、证据行格式、与现有文档的分工
- [ ] `docs/proposals/{README,TEMPLATE,001-adopt-process}.md` 存在,索引与文件状态一致
- [ ] **可用性判据**:不额外解释,AI 能按 `process.md` 为后续第一条工作(002)写出符合模板的草稿,
      且草稿的「完成的判据」每条可由命令或测试验证
- [ ] 守则里每条硬约束都能在 `work-items` §0 或 `issues/README` 或 `CLAUDE.md` 找到出处 —— 本条不发明
      新约束,只收拢

反向断言不适用:本条不改代码。

## 不做什么

- 不动 `doc/`、`docs/` 任何既有文件,不改链接(→ 002)
- 不动 `CLAUDE.md`(→ 003)
- 不加 `make verify`、`hack/check-proposal`、commit-msg hook(→ 004);建成前守则里对应条款靠人核对
- 不加 `.claude/skills/` 下的 `/propose` `/work` `/ship` `/release`(→ 005)
- 不回头给 001 之前的提交补 P 编号;W 编号和 issue 编号那条链自成一段
- 不做成独立 skill 仓库(领导 2026-09-16 定:先在本仓库内规范)

## 不能说什么

不适用于报告文案。对守则本身的一条:**不许写"守则保证正确"**。守则只保证每次状态变更都留了可核对的证据,
核对是人的事。

## 工作项

| W | 一句话 | 提交 |
|---|---|---|
| 1 | 写 `process.md`、`proposals/README`、`TEMPLATE`、本文 | (本条) |
| → 002 | 合并 `doc/` 与 `docs/`:先纯 `git mv` 一提交,再只修链接一提交;`work-items` 冻结 | 独立 proposal |
| → 003 | 拆 `CLAUDE.md`:防护点按包搬进 `.claude/rules/*.md` 带 `paths:`,内容零改动,`CLAUDE.md` < 10 KB | 独立 proposal |
| → 004 | 强制装置:`make verify`、`hack/check-proposal`(证据行)、commit-msg hook(`p/` 分支上要 `(P-NNN)`) | 独立 proposal |
| → 006(原编号 005,被另一会话先占)| 项目本地命令 `.claude/skills/{propose,work,ship,release}/SKILL.md`,每个一屏,内容是"读 `process.md` 第 N 节照做" | 独立 proposal |

002 与 003 是纯搬家,不趁机改写内容 —— 改写和搬家混在一个 PR 里,review 时看不出哪些是真改动。

## 未决问题

1. **主干是哪个分支。** 当前工作在 `dev`,领先 `main` 155 个提交,CI 对两者都触发。
   **已决(2026-09-16):主干是 `dev`**,分支从 `dev` 建、合回 `dev`,tag 从 `dev` 打。`main` 的角色未定义,
   本条不处理;如需退役或改为发布镜像,另开 proposal。
2. **W 编号是否延续。** **已决(2026-09-16):冻结 `work-items`**,新工作项写在 proposal 正文里。老条目
   W-017–W-020 未开始,做的时候各转成一份 proposal,`来源` 写原 W 号。冻结动作本身随 002 一起做。
3. **proposal 语言。** 现有规划文档全中文,守则和 proposal 沿用中文;`CLAUDE.md` 里"代码和用户可见字符串只用英文"
   不受影响。未明确否决,按此执行。

## 完成

```
合入:`docs(process): adopt the development process …`(2026-09-16;sha 随 rebase 变过两次,以 `git log --grep P-001` 为准)
发布:不随版本发布,文档即生效
证据:docs/process.md 6497 B < 8 KB;四个文件在,索引与文件状态一致;
      可用性判据 —— 002 草稿(docs/proposals/002-merge-doc-dirs.md)由 AI 按 process.md 与 TEMPLATE 写出,
      未额外解释,判据每条对应一条命令或测试;
      守则硬约束出处:§4 五条 ← work-items §0 交回检查单 + 五条硬约束;§5 状态集 ← issues/README 状态取值;
      §6 反向断言 ← work-items §1 条目固定字段;阶段 5 顺序 ← CLAUDE.md npm 一节
```
