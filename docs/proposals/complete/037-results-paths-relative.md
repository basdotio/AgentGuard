<!-- SPDX-License-Identifier: MIT -->
# 037 — 已提交的测量结果带着同事的家目录和本机临时路径,一份账本里 1,669 处

- **来源**:开源前清理扫描(2026-09-30)。同事要求 `baselines/results/` 保留,隐私用相对路径解决
- **依赖**:P-015(账本与 fixtures 的 `detail` 字段)、P-021/P-025(判官 run 的 `raw/` 提交进仓)
- **分支**:`p/037-results-paths-relative`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`baselines/results/` 是要随仓库公开的,而它里面写着人和机器:

| 泄漏 | 处数 | 在哪 | 来源 |
|---|---|---|---|
| 同事的语料 checkout `/Users/pan/work/rido/bas/dai/agent-artifact-corpus/…` | 1,667 | `skill-scanner/2026-09-24/ledger.jsonl`(897)、`…-excl-cisco/ledger.jsonl`(770) | cisco adapter 把 skill-scanner 的 stderr 原样写进 `detail` |
| 同事的 `~/.local/share/uv/…` | 2 | `skill-scanner/2026-09-24/fixtures.jsonl` | Python traceback 原样进 `detail` |
| 每次运行的临时工作目录 `/var/folders/<用户哈希>/T/baseline-<n>/…`,含 `/private` 变体 | 12,062 | 四个 `-llm-` run 的 `raw/`,ccaudit 和 skill-scanner 的 `fixtures.jsonl` | aguard 输出的 `root`/`path`/`locations` 是绝对路径,adapter 原样落盘;fixtures 的 `detail` 引用工具报错 |

用户名和内部目录名是身份信息;`/var/folders/<哈希>` 是 macOS 每个用户唯一的临时目录,能指纹到人。而且**每次重跑都会再漏**:
四个 adapter 各自拼 `detail`,没有一处统一收口。

这些路径每个 run 只有一个固定前缀。换成占位符,`<work>/<sample>/home/.claude` 仍然说清了"在暂存树的哪里",只丢掉"谁的机器"。

## 初步方向

一个小包 `baselines/scrub`:按前缀替换,连带 symlink 解析后的形态(macOS 把 `/var/folders` 报成 `/private/var/folders`),长前缀优先。
两处接入:driver 在写盘前对所有 `Row.Detail` 和 `FixtureResult.Detail` 统一替换(`<corpus>`、`<work>`、`<home>`、`<tmp>`),
adapter 不必各自记得;aguard adapter 的 `keepRaw` 对 raw JSON 替换 `<work>`。已提交的结果一次性改写。

## 完成的判据

- [ ] `TestPlaceholdersReplaceEveryFormOfAPrefix`(`baselines/scrub`):原形与 symlink 解析形都换;work 在 tmp 之下时换成 `<work>` 而非 `<tmp>/baseline-…`;
      空路径不当前缀;`Bytes` 对 JSON 文本同样生效。
- [ ] `TestResultsCarryNoMachinePaths`(`baselines/cmd/baseline`):账本行与 fixture 的 `detail` 里 corpus 和 work 前缀都被换掉。
- [ ] `TestRawOutputNamesNoWorkDirectory`(`baselines/adapter/aguard`):`keepRaw` 写出的文件不含 `Work`,`root` 和 `path` 以 `<work>/` 开头。
- [ ] 已提交结果改写后:`grep -rl '/var/folders/' baselines/results` 与 `grep -rl '/Users/pan' baselines/results` 均为 0。
- [ ] 反向断言一:所有 `verdicts.jsonl` 与 `scorecard*.txt` 的 md5 改写前后相同(32 个文件);改写只碰 `detail` 和 raw。
- [ ] 反向断言二:第三方样本内容里引用的路径(`/Users/andreasbigger/.claude/hooks/…` 等)**保留**,它们是样本自己写的,是证据不是泄漏。
- [ ] 改写过的每个 `.jsonl` 逐行、每个 `.json` 整体仍能 `json.loads`。
- [ ] `make verify` 绿。

## 不做什么

- 不改任何 verdict、severity、dimensions、rules;不改 run.yaml;不改 scorecard。
- 不删、不合并任何 run 目录(留多少是另一条,见清理报告决定三)。
- 不动第三方样本内容里的路径,不动 `/tmp/cisco-venv` 这类工具安装位置(不指向人)。
- 不改 adapter 各自拼 `detail` 的措辞;替换在 driver 统一做。
- 不动 `internal/`,不动语料。

## 不能说什么

- 不说"results 里没有第三方路径":有,是样本内容,README 写明了区别。
- 引用 results 里的路径时用占位符形态,不再复原。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 三个测试,HEAD 上编译不过(`scrub.New`、`scrubResults` 未定义) | `baselines: tests — results name no machine: corpus, work, home and temp become placeholders (P-037)` |
| 2 | `baselines/scrub` 包;driver 写盘前统一替换;aguard `keepRaw` 替换 `<work>` | `baselines: results name no machine — corpus, work, home and temp are placeholders in every detail and in raw/ (P-037)` |
| 3 | 改写已提交的 results:1,836 个文件,13,737 处 | `baselines: rewrite the committed results — 13,737 machine paths become <corpus>, <work> and <home>; verdicts and scorecards byte-identical (P-037)` |
| 4 | README 一节、本文件、索引 | `baselines: say what the placeholders in results stand for (P-037)` |

## 未决问题

1. **`/tmp/cisco-venv` 和 `/Users/pan/.local/share/uv` 这类工具安装路径要不要也换?** 建议:`/Users/pan/...` 换成 `<home>/...`(指向人),
   `/tmp/cisco-venv` 不换(不指向人,且说明了工具是怎么装的)。**已决(2026-09-30)**:按建议。
2. **要不要在 CI 里加一条"results 不得含 `/Users/` 或 `/var/folders/`"的检查?** 建议加进 `make verify`,一行 grep。但 `/Users/` 会误伤第三方样本内容,
   所以只查 `/var/folders/` 和维护者的用户名不现实。**已决(2026-09-30)**:本条不加,driver 统一替换已堵住源头;留给 CI 的话另起。

## 完成

```
合入:PR(2026-09-30;sha 合入后用 git log --grep P-037 找)
发布:待发
证据:TestPlaceholdersReplaceEveryFormOfAPrefix(baselines/scrub/scrub_test.go)、TestResultsCarryNoMachinePaths(baselines/cmd/baseline/scrub_test.go)、
     TestRawOutputNamesNoWorkDirectory(baselines/adapter/aguard/raw_test.go);W1 在 HEAD 上编译失败(undefined: New / scrubResults),W2 后绿;
     改写 1,836 个文件 13,737 处后 grep '/var/folders/' 与 '/Users/pan' 均 0 个文件;反向断言:32 个 verdicts/scorecard 的 md5 前后相同,
     /Users/andreasbigger 仍在 2 个文件里;raw 的 root 现为 "<work>/mal-conn-mcp-autorun-bypass/home/.claude"
```
