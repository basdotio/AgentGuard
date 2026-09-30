<!-- SPDX-License-Identifier: MIT -->
# 004 — 强制装置:一条 `make verify`、两个 git hook、守则的三处修订

- **来源**:P-001 工作项 → 004;002/003 跑出来的三条守则修订意见
- **依赖**:P-003(已完成)
- **分支**:`p/004-enforcement`

## 问题

守则里有三处写着"第 4 步建成前靠人核对":

1. **闸门是五条命令不是一条。** `process.md` §4 列了 test / lint / docs 无漂移 / plugin 自扫 / `go` 指令,002 和 003 各跑了
   两遍,每次手拼。CI 里有同样五步但顺序和写法不同,是第二份定义。
2. **`p/` 分支上的提交信息没有 `(P-NNN)` 不会被拒。** 回溯链靠它,漏一条就断一环,现在靠 AI 记得。
3. **状态改成 `已完成`/`已修复` 可以不带证据行。** `issues/README` 记过两次这种事故,`process.md` §6 说"不合入",
   但没有东西真的拦。

另外两个 proposal 跑下来,守则本身有三处要改,按 §8 攒到这里:

- **工作项表里不写 sha**(002 未决 8):rebase 一次全变。
- **「完成」一节只能在合入后直接提交到主干**,按现在的例外条款(不改状态字段)这是违规;002、003 各违了一次。
- **交付前必须 fetch 并 rebase 到主干**,冲突由 AI 解决并在 review 包里披露(002 碰到了,守则没写)。

## 目标

```
make verify        # 依次:go vet · test · lint · docs 无漂移 · plugin 自扫 · go 指令 == 1.23.5;任一失败即停
make hooks         # 把 hack/pre-commit 与 hack/commit-msg 复制进 .git/hooks/(clone 之后跑一次)
hack/commit-msg    # 当前分支匹配 p/* 时,提交信息必须含 (P-NNN),否则拒绝并打印格式
hack/check-proposal# 被 hack/pre-commit 调用:暂存的 docs/proposals/*.md、issues/*.md 里,新增的
                   # "状态"行若为 已完成/已修复/部分修复,同文件必须有一行以"证据:"开头;proposal 的状态必须在六个取值内
```

现有 `hack/pre-commit` 的 aguard 闸门行为不变,只在末尾多调一次 `check-proposal`。

## 完成的判据

- [ ] 干净树上 `make verify` exit 0;把 `docs/rules.md` 改一个字后 exit 非 0(docs 漂移那步真的在拦)
- [ ] `TestCommitMsgHookRequiresProposalID`:临时 git 仓里,分支 `p/999-x` 上无 ID 的信息被拒、带 `(P-999)` 的放行;
      分支 `dev` 上无 ID 放行(hook 只管 `p/`)
- [ ] `TestCheckProposalDemandsEvidence`:暂存一份把状态改成 `已完成` 且无证据行的 proposal → 拒;加一行 `证据:` → 放行;
      状态写成六个之外的词 → 拒
- [ ] **反向断言**:两个 hook 各有"正确输入必须放行"的用例,防止拦截被写成拦一切
- [ ] `process.md` 三处修订落地:§3 例外条款加第四种情形(合入后的「完成」提交);§3 加"交付前 rebase 到主干";
      工作项表模板(`TEMPLATE.md`)的「提交」列改名「提交信息」并注明不写 sha;§4 改为指 `make verify`;
      三处"第 4 步建成前靠人核对"删掉
- [ ] `CLAUDE.md` 常用命令加 `make verify`、`make hooks` 两行(这是 003 之后 CLAUDE.md 唯一允许的增量:命令表)
- [ ] `TestDocsRelativeLinksResolve`、`TestClaudeRulesAreScopedToExistingPaths` 仍绿;`plugin/` 零改动

## 不做什么

- 不改 CI(`.github/workflows/ci.yml`)。CI 按步骤拆开是为了每步单独可见、可缓存;`make verify` 是本地一条命令。
  两份定义并存,靶子是一致的五条,漂移风险记在未决问题 1
- 不改 `hack/pre-commit` 现有的 aguard 闸门逻辑
- 不在 `dev` 或其他非 `p/` 分支上强制提交信息格式
- 不加 `.claude/skills/`(005)
- `make verify` 不含真机扫描(`aguard scan --root ~/.claude`),那条仍是 collect/detect 改动后手动跑,守则不变

## 不能说什么

不适用于报告文案。对本条:hook 是**本机**装置,clone 后不跑 `make hooks` 就没有;守则和 `CLAUDE.md` 里要写清
"hook 拦不到没装 hook 的机器",不能写成"提交已受保护"。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 两个 hook 的测试(临时 git 仓 + `os/exec`),对当前树跑红(脚本不存在) | `test(hooks): commit-msg demands a proposal id on p/ branches, check-proposal demands evidence …` |
| 2 | `hack/commit-msg`、`hack/check-proposal`,`hack/pre-commit` 末尾接入;测试转绿 | `hack: commit-msg and check-proposal hooks …` |
| 3 | `Makefile` 加 `verify`、`hooks`;`CLAUDE.md` 命令表加两行 | `make: verify runs the whole gate in one command; hooks installs both hooks` |
| 4 | `process.md` 三处修订、`TEMPLATE.md` 提交列、删"建成前靠人核对" | `docs(process): the three revisions 002 and 003 earned …` |

## 未决问题

1. **CI 与 `make verify` 两份定义。** 建议先并存;若哪天 CI 加了第六步而 verify 没加,那是下一条 proposal,不预防。
2. **hook 的语言。** 现有 `pre-commit` 是 bash,新的两个跟它一致用 bash;测试用 Go 起子进程跑,和 npm launcher 的测试同一套路。
3. **`check-proposal` 只看暂存的 diff 新增行。** 已经写错的历史状态不追。
4. **`hack/pre-commit` 改了一处不在白名单里的东西**:`mapfile` 换成 `while read`。macOS 自带 bash 3.2 没有 `mapfile`,
   原 hook 在这台机器上会以 "command not found" 失败。aguard 闸门逻辑一字未动,只是让它能跑。在此披露。
5. **负向测试第一版做错了**:在工作树里追加垃圾到 `rules.md`,`gen-rules` 先重新生成把它盖掉,`verify` 照样绿。
   正确做法是把改坏的文件暂存进 index(模拟"已提交的 rules.md 过时"),`verify` exit 1。这条写进了 `process.md` §4。
6. **`make hooks` 已在本机执行**,`.git/hooks/` 里现在是这两份;这是 004 的目的,但它改的是你机器上不进 git 的文件。
7. `commit-msg` 接受 `(P-NNN)` 三位及以上;`p/release-*` 分支同样要求 ID,release proposal 也是 proposal。

## 完成

```
合入:927dd4d(2026-09-16,ff 合入 dev;分支已删)
发布:不随版本发布
证据:TestCommitMsgHookRequiresProposalID + TestCheckProposalDemandsEvidence(cmd/aguard/hooks_test.go);
      make verify 干净树 exit 0、暂存过时 rules.md 后 exit 2;本机 hook 实拒一条无 ID 提交;
      process.md "建成前" 0 处;本条「完成」提交本身穿过了 check-proposal(状态 已完成 + 本证据行)
```
