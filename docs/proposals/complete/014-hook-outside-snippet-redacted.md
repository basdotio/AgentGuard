<!-- SPDX-License-Identifier: MIT -->
# 014 — hook 越界提示的证据里,箭头后面的解析路径没经过脱敏

- **来源**:`HOOK-002` 的 snippet 由 `redactClip(ref) + " → " + resolved` 拼成,解析出的路径没经过脱敏,前半段被脱掉的东西
  会在箭头后原样出现;`SUP-006` 的 `Why` 里注册表地址同理。移植自旧仓 agent-guard 的 P-056(私有仓)
- **依赖**:无
- **分支**:`p/014-hook-outside-snippet-redacted`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

hook command 引用的脚本解析到 HOME 外时,`HOOK-002` 的证据片段这样拼(`internal/detect/hooks.go:361`):

```go
Snippet: clip(redactClip(ref) + " → " + resolved)
```

箭头前面的 `ref` 是从 hook command 里抄出来的,过了 `redactClip`;箭头后面的 `resolved` 是同一串字节展开
`~`/`$HOME`/`$CLAUDE_PROJECT_DIR`、或拼上 home 之后的结果,**一个字节都没脱敏**。绝对路径的引用 `resolved` 就等于 `ref`。
所以脱敏器在前半段抹掉的东西,后半段原样印出来——不变量 #3 说的"`detect.Redact` 是产出 snippet 的唯一途径"在这一处不成立。

复现(本仓 `main` dec64ca 构建的二进制,`HOME` 指向 fixture 里的 home,两条 hook:`sh <HOME 外>/opt/ghp_<36 位>/hook.sh`
和 `sh <HOME 外>/outside/guard.sh`,`scan --json --inbox off`):

- 带 token 的那条 `HOOK-002` 的 snippet:`<REDACTED><REDACTED>/hook.sh → /tmp/aguard-scratch/p014fx/opt/ghp_<36 位>/hook.sh`
  —— token 在箭头后原样出现
- 同一个引用在 `COV-000` note 里:`<REDACTED><REDACTED>/hook.sh` —— 同一串字节,这里是脱敏的
- 另一条普通路径的 `HOOK-002`:`<REDACTED>.sh → /tmp/aguard-scratch/p014fx/outside/guard.sh` —— 普通长路径也会被熵检测
  整段吃掉前半段,而后半段照印

这条 snippet 进 JSON、HTML、markdown(`check --md -` 是写来贴 PR 评论的)、SARIF(上传到代码扫描)和终端报告。
判官那条路两处都会对证据再脱敏一次(`judge/run.go` 的 `triageItems`、`judge/excerpt.go`),所以泄漏面是报告,不是模型端点。

**同类排查**(在 `internal/detect`、`internal/collect`、`internal/permcheck` 里找"同一段字节一处脱敏、另一处原样"):
`SUP-006`(包源被改)的 `Why` 把从文件行里取出的目标拼进句子(`internal/detect/shape.go:194-204`),而同一行在 snippet 里是脱敏的。
目标解析不出主机时(端口是 `${PORT}` 这类 `url.Parse` 拒绝的写法、userinfo 后面没有主机、`${VAR:-…}` 的默认值坏了),
拼进去的是原样的目标串。同一个 fixture 里一个 skill 脚本写 `npm config set registry https://ci:ghp_<36 位>@npm.corp:${PORT}/`:
snippet 是 `npm config set registry https://ci:<REDACTED>@npm.corp:${PORT}/`,`Why` 里是完整的 token。
整份 JSON 里 token 出现 2 次(`HOOK-002` 的箭头后一次、`SUP-006` 的 `Why` 一次)。

后果:用户把一个 secret 放进了路径或 URL(目录名、registry 地址里的凭证),工具在一处替他抹掉、在同一条发现的另一处替他印出来;
报告越是被转贴(PR 评论、SARIF 上传),这一处越不该是例外。

## 初步方向

`HOOK-002`:先把整串 `ref → resolved` 拼好,再整体过一次 `redactClip`(先脱敏后截断的顺序不变);普通路径的 snippet 与今天逐字相同。
`SUP-006`:拼进 `Why` 的目标先脱敏。`detect.Redact` 本身不动。其余同类形状的地方逐一列出,形状不同的不在本条修。

## 完成的判据

fixture 都在 `t.TempDir()` 现搭;token 用 `ghp_` 加 36 位的明显假值(`redact_test.go` 已有同样写法),`Redact` 的已知前缀表能认出它。

- [x] `TestHookOutside_SecretInPathIsRedactedOnBothSides`(`internal/detect/snippet_redact_test.go`,新):hook `sh <HOME 外>/opt/<token>/hook.sh`
  走 `Engine.Run`,`HOOK-002` 和 `COV-000` 的 Title、Why、Evidence 的 File 与 Snippet 里都没有 token;`HOOK-002` 的 snippet 仍是
  `… → …` 的形状、两边都以 `/hook.sh` 结尾、箭头后面带 `<REDACTED>`。再直接调 `hookOutsideFinding` 钉两种字面值:绝对引用
  (`/opt/vault/<token>/hook.sh` 两边相同)和两边不同的引用(`$HOME/../vault/<token>/hook.sh → /home/vault/<token>/hook.sh`),
  修后分别是 `/opt/vault/<REDACTED>/hook.sh → /opt/vault/<REDACTED>/hook.sh` 和 `$HOME/../vault/<REDACTED>/hook.sh → /home/vault/<REDACTED>/hook.sh`。
  今天红:箭头后 token 原样
- [x] `TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts`(同文件,新):三种"目标读不出主机"的写法(`${PORT}` 端口、userinfo 后无主机、
  `${VAR:-…}` 默认值坏了)外加一个 token 形状的主机名,`SUP-006` 的 `Why` 里都没有 token,而且 snippet 本来就没有。今天红
- [x] `TestScan_PathSecretsNeverReachARendering`(`cmd/aguard/redact_render_test.go`,新):同一份 fixture(上面那条 hook,加一个 skill 脚本里
  `npm config set registry https://ci:<token>@npm.corp:${PORT}/`)走 `scanEnv`,JSON(与 CLI 同样的缩进编码)、终端(普通与 `--verbose`)、
  markdown、SARIF、HTML 六种渲染里都没有 token,`HOOK-002` 和 `SUP-006` 都在。今天红
- [x] 反向断言:普通路径的 `HOOK-002` snippet 与今天**逐字相同** —— `TestHookOutside_OrdinaryPathSnippetUnchanged`(同 detect 文件)用表钉住字面值
  (`/opt/acme/hooks/guard.sh`、带空格的 `/Applications/… 3.app/…/unibase-hook.js`(`hooks.go` 头注释里那种真实形状)、两边不同的 `~/…` 展开、
  一条超过 200 字节截断上限的),并且每行都等于在测试里按今天的公式 `clip(redactClip(ref) + " → " + resolved)` 算出的结果;修前修后都绿
- [x] 反向断言:`HOOK-002` 普通事件 dim 4 medium、`PermissionRequest` dim 4 high 不变(上一条的表两种事件各跑一遍);`SUP-006` dim 5 high 不变,
  普通 `${CORP_REGISTRY}` 和已知主机 `https://npm.evil.example/` 的 `Why` 逐字不变 —— `TestRegistryRedirect_OrdinaryWhyUnchanged`(同 detect 文件)
  字面值钉住;修前修后都绿
- [x] 反向断言:既有测试一字不改仍绿 —— `internal/detect/hooks_test.go`(含 `TestHookNotes_DoNotRedactTheScannerOwnPaths`,它钉着 `hookOwnedNote`
  的解析路径**不**脱敏)、`shape_test.go`、`shape_severity_test.go`、`redact_test.go`;`git diff --stat origin/main` 对这四个文件为空
- [x] 真机 `scan --root ~/.claude`:修前修后 JSON 去掉 `scanned_at`、`tool_version` 后逐字节相同,或差异只落在本条改动的两处字段上
- [x] `.claude/rules/detect.md` 仍不超过 200 行(`cmd/aguard/claude_rules_test.go` 的 `maxRuleLines`)
- [x] `make verify` 绿;`go version` 不切换工具链

## 不做什么

- **不改 `detect.Redact`、`redactClip`、`clip`**,也不改熵检测的字符类(路径里的 `/` 让长路径整段被当成一个 token,那是 `Redact` 的性质,见未决 1)
- **不改 `hookOwnedNote`**(`internal/detect/hooks.go:325`):字面上同一个形状,但它的解析路径是插件树**内**的文件路径,
  按引擎"扫描器定位的路径只截断"的约定是有意不脱敏的,有测试钉着(未决 2)。只在 `hookOutsideFinding` 的注释里写清两者为什么不同
- **不改 collect 的笔记**:`imports.go` 四条 note 的 `"@" + ref`、`plugins.go:192` 的插件名、`collect.go` 的 `err.Error()` 和条目列表、
  `connectors.go`/`unowned.go`/`loaded.go` 的文件名 —— 整条原样,没有"一半脱敏"的形状,而且 collect 不能 import detect(未决 3)
- **不改 `unreadableNote`**(`internal/detect/detect.go:870`):条目名原样进 Why 和 snippet,同类的 `nonRegularNote` 却脱敏 ——
  不一致,但整条原样、不是一半一半;条目名是树内路径
- **不改 `internal/gate/status.go:234`** 的 `missing hook command: <cmd>`(不在排查的三个包里;整条原样)
- 不改 `HOOK-002` 的严重度、维度、标题、Why,不改 `COV-000` 笔记;规则文案没动,`docs/rules.md` 不变
- spec 不改:不变量 #3 本来就这么要求,本条是让实现回到它;数据模型不动
- 不加依赖,`go.mod` 不动

## 不能说什么

- 不说"secret 发给了判官":判官两条路都对证据再脱敏一次(`judge/run.go` 的 `triageItems`、`judge/excerpt.go`),`Why` 不进判官;
  泄漏面是本机生成的报告,以及用户转贴、上传的副本
- 不说"所有发现字段现在都脱敏了":collect 的笔记、`unreadableNote`、`hookOwnedNote` 的解析路径、所有 `Evidence.File` 仍原样(见不做什么)
- 不说修后 `HOOK-002` 的证据总比以前好读:解析路径本身会被熵检测当成 token 时(「问题」里那条 `…/outside/guard.sh`),
  修前箭头后面还能看到路径,修后两边都是 `<REDACTED>.sh`。这是拿可读性换"不在另一半漏出来",未决 1
- 不说 SARIF 指纹不变:`partialFingerprints` 含 snippet,解析路径会被脱敏改动的那些 `HOOK-002` 指纹会变(代码扫描里旧告警关闭、新告警打开);
  普通路径的不变
- 不说真机上有 `HOOK-002` 被修了:本机 `~/.claude` 0 条 `HOOK-002`;唯一一条 `SUP-006` 的目标 `Redact` 不改它

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 三条新测试加反向断言(两个包),跑红 | `detect, cmd: tests — a secret in a hook's out-of-home script path or a registry URL reaches the report in clear beside its redacted copy (P-014)` |
| 2 | `HOOK-002` 整串拼好再 `redactClip`;注释写清它与 `hookOwnedNote` 为什么不同 | `detect: HOOK-002 redacts the resolved path after the arrow the same way it redacts the reference before it (P-014)` |
| 3 | `SUP-006` 拼进 `Why` 的目标先过 `Redact` | `detect: SUP-006 names the registry target in its explanation only after redacting it, as the snippet already does (P-014)` |
| 4 | `.claude/rules/detect.md` 的 hook 段补一句防护点(不加行:文件有 200 行上限,本仓现 198 行,P-010 在同一段加两行) | `rules: detect.md says the HOOK-002 snippet is redacted whole and why the plugin attribution note is not (P-014)` |
| 5 | 本文件、索引 | `proposals: P-014 (P-014)` |

## 未决问题

1. **解析路径本身会被熵检测吃掉时,修后箭头两边都是 `<REDACTED>`,接受吗?**
   **建议**:接受。`Redact` 只看形状,分不清"长路径"和"没有前缀的 secret";在 `HOOK-002` 这里替它分,就是在 `Redact` 之外长出第二个出口,
   正是不变量 #3 不许的。补救只能在 `Redact` 本身(比如熵检测字符类里的 `/`),那要另开并重新量误报,本条不动。代价有界:
   带空格的 `/Applications/… 3.app/…/unibase-hook.js` 这种真实形状 `Redact` 不改,修后仍可读(反向表钉着)。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
2. **`hookOwnedNote` 字面上同形,要不要一起改?**
   **建议**:不改,在 `hookOutsideFinding` 的注释里写清区别。`hookOwnedNote` 的解析路径是插件树内的文件,那棵树整棵被扫,其中任何发现的
   `Evidence.File` 都原样印同样的路径 —— 引擎的约定是"扫描器定位的路径只截断",`TestHookNotes_DoNotRedactTheScannerOwnPaths` 钉着它,
   测试注释里记着真实报告上那条被熵检测吃成 `<REDACTED>.5.<REDACTED>.cjs` 的插件路径。`HOOK-002` 的解析路径不在任何被扫的树里,
   字节只来自 hook command,是 snippet 那一侧。要连树内路径也脱敏,是"`Evidence.File` 要不要脱敏"的全引擎问题,另开。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
3. **collect 的原样笔记(`@import` 引用、插件名、错误串、文件名)要不要另开?**
   **建议**:本条只列出、不修;要修另开。修法得先定 `Redact` 放在哪:collect 不能 import detect(detect 依赖 collect),要么把 `Redact`
   挪到一个两边都能用的叶子包,要么由 detect 在合并 collect 笔记时统一过一遍 —— 两条路都改 note 的产出路径,与本条"把一处漏网的拼接收回唯一出口"
   不是一件事。其中最接近真实泄漏的是 `imports.go` 的 `@ref`(指令文件正文)。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
4. **`SUP-006` 的目标用 `Redact` 还是 `redactClip`?**
   **建议**:`Redact`,不截断。`Why` 里的目标今天不设长度上限;加截断会让超长而不含 secret 的目标在 `Why` 里变短,普通情况就不再逐字相同。
   只脱敏、不截断不存在先后问题(不变量 #3 管的是两步都做时的顺序);permcheck 的 snippet 也是只 `Redact`(`permcheck.go:104`)。
   **已决(2026-10-09)**:按建议(旧仓已决,移植沿用)。
5. **`SUP-006` 跟 hook 放在同一个 PR,还是拆出去?**(标题只说 hook)
   **建议**:同一个 PR。它是本条要求的同类排查找到的、同一个修法(把漏网的那一半收回 `Redact`),独立成 W3 一个提交、带自己的红绿测试,
   要单独回退只需回退那一个提交。
   **已决(2026-10-09)**:按建议(旧仓实现中提出并已决,移植沿用)。
6. **"整串一次脱敏 = 两半各脱一次"是论证,不是穷举,够不够?**
   普通路径逐字不变的依据是:没有一条 `Redact` 模式能跨过 ` → `(`→` 不在任何值字符类里;能把它当值吃进去的 `flagSecretRE`/`flagUserPassRE`
   要求前面紧跟 `--token`、`-u` 这类旗标,而 `ref` 必以脚本扩展名结尾)。
   **建议**:够。将来加一条能跨过空白的模式,整串形式只会脱得更多(安全方向);普通路径的逐字不变由 `TestHookOutside_OrdinaryPathSnippetUnchanged`
   按旧公式逐行比较兜底,哪天不成立会红。
   **已决(2026-10-09)**:按建议(旧仓实现中提出并已决,移植沿用)。

## 完成

```
合入:PR 待开(2026-10-09;sha 合入后用 git log --grep P-014 找)
发布:待发
证据:TestHookOutside_SecretInPathIsRedactedOnBothSides(internal/detect/snippet_redact_test.go);W1 在本仓 main(dec64ca)上红:Engine.Run 下 HOOK-002 的 snippet 是 <REDACTED><REDACTED>/hook.sh → /var/folders/…/opt/ghp_<36 位>/hook.sh,两条字面值子测试箭头后 token 原样 → W2 后绿,箭头两边都是 …/<REDACTED>/hook.sh
证据:TestRegistryRedirect_WhyRedactsWhatTheSnippetRedacts(同文件);W1 红,四个子测试(${PORT} 端口、userinfo 后无主机、${R:-…} 默认值坏了、token 形状的主机名)Why 里都是完整 token → W2 后仍红(不归它管)→ W3 后绿
证据:TestScan_PathSecretsNeverReachARendering(cmd/aguard/redact_render_test.go);W1 红:JSON、终端、--verbose、markdown、HTML 各 2 处,SARIF 3 处 → W2 后各 1 处、SARIF 2 处(剩 SUP-006 的 Why)→ W3 后六种全 0;HOOK-002、SUP-006 都还在
证据:反向断言 TestHookOutside_OrdinaryPathSnippetUnchanged(同 detect 文件):四条普通路径 × 两种事件共 8 个子测试,snippet 逐字等于旧公式算出的值、HOOK-002 dim 4 medium/high,W1 时就绿,修后不改一字仍绿;变异(临时把修法换成 redactClip(ref) + " → " + redactClip(resolved),跑完还原,未提交)→ 超过 200 字节那两行红(PreToolUse、PermissionRequest 各一)
证据:反向断言 TestRegistryRedirect_OrdinaryWhyUnchanged(同文件):${CORP_REGISTRY} 与 npm.evil.example 的 Why 字面值、SUP-006 dim 5 high,W1 时就绿,修后仍绿
证据:反向断言 TestHookNotes_DoNotRedactTheScannerOwnPaths(internal/detect/hooks_test.go,未改)W1–W4 每一步都绿
证据:二进制前后(dec64ca vs 本分支,fixture 在 /tmp 下,三条 HOME 外 hook + 一个 npm registry 改写脚本,HOME 指向 fixture,--inbox off):scan --json 去掉 scanned_at、tool_version 后 178 行里只差 3 行 —— SUP-006 的 Why 里 ghp_<36 位> → <REDACTED>;带 token 的 HOOK-002 箭头后 ghp_<36 位> → <REDACTED>;路径整段被熵检测吃掉的那条 <REDACTED>.sh → /tmp/aguard-scratch/…/outside/guard.sh 变成 <REDACTED>.sh → <REDACTED>.sh(未决 1 接受的代价)。token 出现次数 2 → 0;普通短路径那条 /tmp/p014h/guard.sh → /tmp/p014h/guard.sh 逐字相同;overall 69 → 69
证据:真机 ~/.claude,两个二进制背靠背跑:修前修后 overall 69 / artifact 175 / 发现 806 / note 10 / HOOK-002 0 条 / SUP-006 1 条,JSON 去掉 scanned_at、tool_version 后逐字节相同(15678 行)
证据:不做什么 —— git diff --stat origin/main -- internal/detect/redact.go internal/detect/detect.go internal/collect internal/gate internal/permcheck docs/rules.md docs/spec go.mod go.sum internal/detect/hooks_test.go internal/detect/shape_test.go internal/detect/shape_severity_test.go internal/detect/redact_test.go 为空;hooks.go 的两个 hunk 都在 hookOutsideFinding(它上方的注释与函数体),hookOwnedNote 一字未动
证据:.claude/rules/detect.md 198 → 198 行(一行内补句,不加行),TestClaudeRulesAreScopedToExistingPaths 绿
证据:make verify: all gates passed;go version go1.23.5(无工具链切换),go.mod 第二行 go 1.23.5
```
