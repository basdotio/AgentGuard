<!-- SPDX-License-Identifier: MIT -->
# 008 — 扫描结果贴不进 PR 评论和 issue:只有终端文本和 HTML,想贴就得手抄

- **来源**:新发现(2026-09-20,对话提出)
- **依赖**:无
- **分支**:`p/008-markdown-report`(阶段 2 建成后填)

## 问题

一个人在 PR 里加了一个 skill,reviewer 想把 `aguard check` 的结果贴进评论;或者有人在 issue 里报"这个插件被
aguard 标了 EXFIL-003"。现在能拿到的只有三样:终端文本(对齐空格、`←`、缩进,贴进 markdown 之后表格没有、
缩进塌掉、`[EXEC-001]` 被当成链接语法)、自包含 HTML(贴不进评论,只能当附件)、JSON/SARIF(给机器的)。
于是实际发生的是**手抄**:挑几条打字进评论。手抄的后果不是慢,是**漏**:抄的人只抄自己觉得重要的,被压制的
发现、覆盖缺口、"仅提示可疑面"的标注这些本仓库花力气保住的东西,第一个就被抄掉。

## 初步方向

报告已经有一个给人读、按"不写代码的读者"排序的白话层(`internal/report/plain.go`,终端和 HTML 共用)。
加第三个渲染器 `--md`,把同一份派生结果写成 GitHub 风味 markdown,不加任何判断。设计时要定的事:
写文件还是 stdout、`check` 上不上、证据折不折 `<details>`、Downloads 一节贴进公开 PR 会不会泄露本机文件名。
安全上的硬要求只有一条:所有来自磁盘的字符串只能出现在代码跨度里,否则一个 skill 的文件名就能在你的 PR 里
@ 人、放追踪图片、拆表格。

## 完成的判据

- [ ] `TestMarkdown_ReadsForNonSpecialists`(`internal/report/markdown_test.go`)钉住阅读顺序与终端一致:
      `Risk score N/100` → 一句白话结论 → "先看什么"(≤ 3 条)→ 全部发现(STATIC 与 LLM JUDGE 分节)→ 白名单压制 →
      没检查到的 → Scan details;两个分数不同时并列出现,相同时只出现一次
- [ ] `TestMarkdown_AttackerTextIsInert`:一条发现的 snippet、file、artifact 名里放
      `| x |`、`[a](http://evil)`、`@octocat`、`#1`、`<img src=x>`、反引号串、bidi 字符,渲染后这些串**只出现在代码跨度里**,
      表格行数 = 表头 + 发现数(`|` 没有把行拆开),输出里没有 `](`、`<img`、`<script`,bidi 字符不存在
- [ ] **反向断言一**:`TestWriteMarkdown_StillShowsTheDangerousThing`(`cmd/aguard/main_test.go`)用
      `TestWriteReport_DefaultStillShowsTheDangerousThing` 同一个 evil fixture 走真实流水线:结果里最重的规则 ID
      出现在 markdown 里;维度 7/8 的静态命中仍带 `advisory: not confirmed`;覆盖 note 折进 `<details>` 后
      `<summary>` 行仍带条数和最高严重度
- [ ] **反向断言二**:`check --md out.md --fail-on high` 对该 fixture **退出码 1 且文件非空**——文件在闸门之前写,
      和 `--sarif` 同一顺序(最需要贴的那次恰好是失败的那次)
- [ ] `TestMarkdown_IgnoresVerbose`:`--verbose` 开与关,`--md` 输出**逐字节相同**(与 `--json`/`--html` 同一条规则,
      CI 输出不取决于某个人加没加它)
- [ ] `TestSandboxBanner_RendersInTextAndHTML` 扩到 markdown:沙箱横幅在顶部,带依据
- [ ] `TestSARIF_IsByteStable`、`internal/report` 与 `cmd/aguard` 现有全部测试不改一字仍绿——终端与 HTML 输出没变
- [ ] `make verify` 绿;`TestDocsRelativeLinksResolve` 绿(README 两语、spec §3/§9、architecture 两语、
      `.claude/rules/report.md`、`CLAUDE.md` 常用命令各加了 `--md`)
- [ ] **人工核对(合入前)**:把 evil fixture 的 `--md` 输出贴进一个测试 issue,表格渲染、`<details>` 可展开、
      `@octocat` 没有产生 mention 通知

## 不做什么

- **不加任何判断、阈值、新文案**:markdown 只消费 `Aggregate`、`plain.go` 已有的派生函数和 `sanitizeResult`。
  `internal/score`、`internal/detect`、`internal/gate` 零改动(`git diff --stat origin/dev -- internal/score internal/detect internal/gate` 为空)
- **不动终端与 HTML 的输出**:`text.go`、`html.go`、`report.html.tmpl` 若需抽公共函数,只允许搬不允许改;
  现有测试不改一字即证明
- **JSON 与 SARIF 逐字节不变**:它们给机器,不过 `Sanitize`,本条不碰
- **不做 `--report` 式的自动落盘路径**:`--md` 只认显式路径(是否支持 `-` 见未决 1)
- **不为 GitHub 评论的 65536 字符上限做截断或分片**:发现永不裁;证据沿用终端每组 3 条 + 点名余数的预算;
  全机扫描超限是使用场景问题,README 说清"贴的是 `check` 或 `scan --root .` 的结果"
- **不动 `plugin/`** 三个已发布 skill 的 usage 文案(版本号是发版的事;下次 release 顺手加一行)
- **不改 `hack/github-action.yml`**,不加评论 PR 的步骤(要 `pull-requests: write`,fork 上不生效,单独议)
- 规则 ID **不做链接**:`docs/rules.md` 没有按规则的锚点,SARIF 也没配 helpUri;页尾指一次 `docs/rules.md` 即可

## 不能说什么

和另外两个人读渲染器同一条纪律,只是这份会离开本机、进公开的 PR:

- 维度 7(后门)/ 8(资源滥用)的静态命中必须带 `advisory: not confirmed`(spec §16 不变量 6);LLM judge 的发现
  必须在独立一节、标 "advisory, never sets the score";**绝不写 "safe" / "no malware" / "verified"**
- 页尾保留终端那句 "Static analysis only: it cannot prove malice or see runtime behaviour"
- 证据被预算裁掉时**点名余数**("… and N more"),不许写"全部发现如下"
- 压制的发现写"suppressed, not absent",带最高严重度;`Trusted by allowlist` 一节不许省
- 贴出去的 snippet 之所以能贴,是因为脱敏发生在 detect 阶段(不变量 3);本条不引入任何绕过它的路径,
  markdown 也不从原始 `ScanResult` 取字符串,只从 `sanitizeResult` 的副本取

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 上面判据里的六个测试,先红:`internal/report/markdown_test.go`、`cmd/aguard/main_test.go` 两条、沙箱 smoke 扩一行 | `test(report): a markdown report reads in the same order, keeps attacker text inert, and is written before the gate (P-008)` |
| 2 | `internal/report/markdown.go`:`Markdown(w io.Writer, r model.ScanResult) error`,从 `sanitizeResult` + `Aggregate` + `plain.go` 派生;所有磁盘来源字符串只进代码跨度,`\|` 转义,反引号串用更长的围栏包 | `report: --md renders the same report as GitHub-flavoured markdown; attacker text never leaves a code span (P-008)` |
| 3 | `scan`/`check` 加 `--md <path>`,在 `--sarif` 同一处、闸门之前写;stderr 印 `Markdown written to …`;`--verbose` 不影响 | `aguard: --md on scan and check, written before the gate like --sarif (P-008)` |
| 4 | 文档:README 两语加一行用法 + 一段"贴进 PR"(`gh pr comment --body-file`);spec §3 CLI 行、§9 加第三个人读渲染器;architecture 两语一句;`.claude/rules/report.md` 加"三个人读渲染器、一条注入规则";`CLAUDE.md` 常用命令一行 | `docs: --md is the third human-read renderer; the report, spec and rules say what may not leave a code span (P-008)` |


## 完成的判据

- [ ] `TestMarkdown_ReadsForNonSpecialists`(`internal/report/markdown_test.go`)钉住阅读顺序与终端一致:
      `Risk score N/100` → 一句白话结论 → "先看什么"(≤ 3 条)→ 全部发现(STATIC 与 LLM JUDGE 分节)→ 白名单压制 →
      没检查到的 → Scan details;两个分数不同时并列出现,相同时只出现一次
- [ ] `TestMarkdown_AttackerTextIsInert`:一条发现的 snippet、file、artifact 名里放
      `| x |`、`[a](http://evil)`、`@octocat`、`#1`、`<img src=x>`、反引号串、bidi 字符,渲染后这些串**只出现在代码跨度里**,
      表格行数 = 表头 + 发现数(`|` 没有把行拆开),输出里没有 `](`、`<img`、`<script`,bidi 字符不存在
- [ ] **反向断言一**:`TestWriteMarkdown_StillShowsTheDangerousThing`(`cmd/aguard/main_test.go`)用
      `TestWriteReport_DefaultStillShowsTheDangerousThing` 同一个 evil fixture 走真实流水线:结果里最重的规则 ID
      出现在 markdown 里;维度 7/8 的静态命中仍带 `advisory: not confirmed`;覆盖 note 折进 `<details>` 后
      `<summary>` 行仍带条数和最高严重度
- [ ] **反向断言二**:`check --md out.md --fail-on high` 对该 fixture **退出码 1 且文件非空**——文件在闸门之前写,
      和 `--sarif` 同一顺序(最需要贴的那次恰好是失败的那次)
- [ ] `TestMarkdown_IgnoresVerbose`:`--verbose` 开与关,`--md` 输出**逐字节相同**(与 `--json`/`--html` 同一条规则,
      CI 输出不取决于某个人加没加它)
- [ ] `TestSandboxBanner_RendersInTextAndHTML` 扩到 markdown:沙箱横幅在顶部,带依据
- [ ] `TestSARIF_IsByteStable`、`internal/report` 与 `cmd/aguard` 现有全部测试不改一字仍绿——终端与 HTML 输出没变
- [ ] `make verify` 绿;`TestDocsRelativeLinksResolve` 绿(README 两语、spec §3/§9、architecture 两语、
      `.claude/rules/report.md`、`CLAUDE.md` 常用命令各加了 `--md`)
- [ ] **人工核对(合入前)**:把 evil fixture 的 `--md` 输出贴进一个测试 issue,表格渲染、`<details>` 可展开、
      `@octocat` 没有产生 mention 通知

## 不做什么

- **不加任何判断、阈值、新文案**:markdown 只消费 `Aggregate`、`plain.go` 已有的派生函数和 `sanitizeResult`。
  `internal/score`、`internal/detect`、`internal/gate` 零改动(`git diff --stat origin/dev -- internal/score internal/detect internal/gate` 为空)
- **不动终端与 HTML 的输出**:`text.go`、`html.go`、`report.html.tmpl` 若需抽公共函数,只允许搬不允许改;
  现有测试不改一字即证明
- **JSON 与 SARIF 逐字节不变**:它们给机器,不过 `Sanitize`,本条不碰
- **不做 `--report` 式的自动落盘路径**:`--md` 只认显式路径(是否支持 `-` 见未决 1)
- **不为 GitHub 评论的 65536 字符上限做截断或分片**:发现永不裁;证据沿用终端每组 3 条 + 点名余数的预算;
  全机扫描超限是使用场景问题,README 说清"贴的是 `check` 或 `scan --root .` 的结果"
- **不动 `plugin/`** 三个已发布 skill 的 usage 文案(版本号是发版的事;下次 release 顺手加一行)
- **不改 `hack/github-action.yml`**,不加评论 PR 的步骤(要 `pull-requests: write`,fork 上不生效,单独议)
- 规则 ID **不做链接**:`docs/rules.md` 没有按规则的锚点,SARIF 也没配 helpUri;页尾指一次 `docs/rules.md` 即可

## 不能说什么

和另外两个人读渲染器同一条纪律,只是这份会离开本机、进公开的 PR:

- 维度 7(后门)/ 8(资源滥用)的静态命中必须带 `advisory: not confirmed`(spec §16 不变量 6);LLM judge 的发现
  必须在独立一节、标 "advisory, never sets the score";**绝不写 "safe" / "no malware" / "verified"**
- 页尾保留终端那句 "Static analysis only: it cannot prove malice or see runtime behaviour"
- 证据被预算裁掉时**点名余数**("… and N more"),不许写"全部发现如下"
- 压制的发现写"suppressed, not absent",带最高严重度;`Trusted by allowlist` 一节不许省
- 贴出去的 snippet 之所以能贴,是因为脱敏发生在 detect 阶段(不变量 3);本条不引入任何绕过它的路径,
  markdown 也不从原始 `ScanResult` 取字符串,只从 `sanitizeResult` 的副本取

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 上面判据里的六个测试,先红:`internal/report/markdown_test.go`、`cmd/aguard/main_test.go` 两条、沙箱 smoke 扩一行 | `test(report): a markdown report reads in the same order, keeps attacker text inert, and is written before the gate (P-008)` |
| 2 | `internal/report/markdown.go`:`Markdown(w io.Writer, r model.ScanResult) error`,从 `sanitizeResult` + `Aggregate` + `plain.go` 派生;所有磁盘来源字符串只进代码跨度,`\|` 转义,反引号串用更长的围栏包 | `report: --md renders the same report as GitHub-flavoured markdown; attacker text never leaves a code span (P-008)` |
| 3 | `scan`/`check` 加 `--md <path>`,在 `--sarif` 同一处、闸门之前写;stderr 印 `Markdown written to …`;`--verbose` 不影响 | `aguard: --md on scan and check, written before the gate like --sarif (P-008)` |
| 4 | 文档:README 两语加一行用法 + 一段"贴进 PR"(`gh pr comment --body-file`);spec §3 CLI 行、§9 加第三个人读渲染器;architecture 两语一句;`.claude/rules/report.md` 加"三个人读渲染器、一条注入规则";`CLAUDE.md` 常用命令一行 | `docs: --md is the third human-read renderer; the report, spec and rules say what may not leave a code span (P-008)` |

## 未决问题

1. **`--md` 收路径还是写 stdout。** `--json` 独占 stdout,`--html`/`--sarif` 收路径。贴评论最短的路是
   `aguard check ./x --md - | pbcopy` 或 `gh pr comment -F -`。**建议**:收路径,`-` 表示 stdout;传 `-` 时终端报告
   不再印(和 `--json` 同一规则,stdout 只能有一份东西)。 **已决(2026-09-20)**:按建议。
2. **`check` 要不要也有。** PR 场景里最常见的正是"新加一个 skill,CI 跑 `check`"。**建议要**,和 `--sarif` 一样两条命令都上。 **已决(2026-09-20)**:按建议。
3. **折叠用 `<details>` 还是全展开。** GitHub/GitLab/Gitea 都渲染 `<details>`,纯 markdown 阅读器显示为原始标签。
   **建议用**:证据和覆盖 note 折进去,`<summary>` 行带条数与最高严重度(不变量 8 要求的正是这两个数),
   发现本身不折(和终端同一条:发现在任何一档都不折叠)。 **已决(2026-09-20)**:按建议。
4. **发现表格里放哪些列。** **建议**四列:严重度(带图标)、谁 · 什么(`friendlyArtifact` + 标题 + `×N`)、规则 ID、
   位置(`shortPath` 的第一条证据);展开后是 in plain terms / what to do / Why / 全部证据(全路径,不再缩短——
   贴进 PR 的读者没有 `--verbose` 可加)。 **已决(2026-09-20)**:按建议。
5. **flag 名。** 对话里说的是 `--md`;`--markdown` 更完整但多敲。**建议 `--md`**,help 文案写全名。 **已决(2026-09-20)**:按建议。
6. **Downloads 一节要不要进 markdown。** `check` 没有;`scan` 有时它是本机 `~/Downloads` 的内容,贴进公开 PR 会
   泄露本机文件名。**建议**:照渲染但在 README 里说清 `scan --root .` 时它默认还在扫 `~/Downloads`,贴之前
   用 `--inbox off`;本条不改 `--inbox` 默认值。 **已决(2026-09-20)**:按建议。

7. **W1 的锚点在跑红之后改过两处。** 第一版 `TestMarkdown_ReadsForNonSpecialists` 用 `EXEC-001`、`Trusted by allowlist` 做顺序锚点,
   而"先看什么"和 Summary 里就先出现了它们;改成锚在表格行 `| \`EXEC-001\` |` 和小节标题 `## Trusted by allowlist` 上。
   检查器没错,错的是锚点粒度(和 006 未决 4 同类)。
8. **页尾的规则文档链接从 `[docs/rules.md](url)` 改成裸 URL。** `TestMarkdown_AttackerTextIsInert` 断言代码跨度外没有 `](`,
   这条断言比"允许我们自己的链接"更值钱,GitHub 对裸 URL 一样自动成链。
9. **`plain.go` 动了 111 行,但全是搬。** 为了不在 markdown 里重写推导,把四句嵌着磁盘字符串的白话句拆成"数据 + 组句":
   `worstItem`(← `worstLine`)、`trustName`(← `trustNames`)、`escalations`(← `text.go` 的 `escalated`)、`where`(← `actions`)、
   `actionableWorst`(← `writeVerdict`)。终端与 HTML 输出未变:`text_test.go`、`html_test.go`、`plain_test.go`、`sarif_test.go`
   相对 `dev` 零改动且全绿。
10. **W2 和 W3 是一次写完再拆成两个提交的**,不是先 W2 绿再写 W3。cmd 层的 `TestWriteMarkdown_*` 引用 `report.Markdown`,
    W2 落地前整个 cmd 包编译不过,所以这样拆分对绿灯顺序没有影响。
11. **起真实二进制的三个测试**(`TestMarkdownFlag_*`)用 `sync.Once` 在 `os.MkdirTemp` 里 `go build` 一次,测试结束不删那个目录
    (`t.TempDir` 归第一个调用者,`Once` 里拿不到)。每次 `go test` 漏一个二进制在系统 temp 里;CI 是一次性机器,本机靠系统清理。
    先例:仓库里此前没有 e2e 跑 flag 的测试,`--sarif` 只在 report 包测。
12. **页尾链接指向 `blob/dev/docs/rules.md`。** `main` 落后 `dev`,但公开的默认分支是 `main`;领导定合入 `main` 那天要不要改成
    `main`,和 007 留下的默认分支问题一起看。
13. **`--md -` 时终端报告不再印,但 `--json` 与 `--md -` 同时传会两份都写到 stdout。** 没加互斥:`--json` 和 `--html`/`--sarif`
    同传本来就允许(各写各的地方),`-` 是唯一撞 stdout 的情形,两份都要的人不会这么传。要拦的话一行 cobra `MarkFlagsMutuallyExclusive`。
14. **没跑真机扫描**:`internal/collect`、`internal/detect` 零改动,守则 §4 那条不触发。
15. **判据里"贴进测试 issue 人工核对"我做不了**,和 006 的两条人工核对一样待首次使用时验。样张在交付时给出。

## 完成

```
合入:PR #7 https://github.com/basdotio/agent-guard/pull/7(2026-09-20 开;sha 合入后用 git log --grep P-008 找)
发布:v0.10.0
证据:TestMarkdown_ReadsForNonSpecialists、TestMarkdown_AttackerTextIsInert、TestMarkdown_NotesFoldButCarryTheirSeverity、
      TestMarkdown_CleanAndCheck(internal/report/markdown_test.go);TestWriteMarkdown_StillShowsTheDangerousThing、
      TestMarkdownFlag_WrittenBeforeTheGate、TestMarkdownFlag_IgnoresVerbose、TestMarkdownFlag_DashIsStdout(cmd/aguard/markdown_test.go);
      沙箱 smoke 扩到 markdown(sandbox_smoke_test.go);
      人读渲染器 2 → 3,--md 输出在 --verbose 开/关下字节相同,check --md --fail-on high 退出 1 且文件非空;
      反向断言:markdown_test.go 的 advisory 标注与 <summary> 条数/严重度断言,cmd markdown_test.go 的最重规则断言;
      不做什么:score/detect/gate/sarif.go/html.go/模板/plugin/CI 模板相对 dev 零 diff;make verify 全绿;
      未验证:贴进测试 issue 的人工核对(未决 15)
```
