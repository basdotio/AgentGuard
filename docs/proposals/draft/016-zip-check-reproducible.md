<!-- SPDX-License-Identifier: MIT -->
# 016 — 同一个 zip 查两遍,SARIF 不一样:随机解压目录名进了 uri、artifact 和指纹,Code Scanning 每跑一次开一批新告警

- **来源**:同一个 zip 查两遍,SARIF / JSON / 文本报告不一样:随机的解压目录名漏进 artifact 名、uri 和 `partialFingerprints`,
  于是 GitHub Code Scanning 每跑一次 CI 就开一批新告警;`aguard approve x.zip` 记下的是一个已经删掉的临时路径;
  root 形状的 zip 把共享的 `$TMPDIR` 当成 home 来读。移植自旧仓 agent-guard 的 P-048(私有仓)
- **依赖**:无
- **分支**:`p/016-zip-check-reproducible`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`aguard check x.zip` 先把包解到 `os.MkdirTemp("", "aguard-inbox-")`(`internal/inbox/archive.go` `ExtractZip`),再把那个目录当
目标查(`cmd/aguard/main.go` `checkTarget`)。查完只把 `out.Root` 改回 zip 路径,其余字段**原样指向随机的临时目录**。
在 `main`(`dec64ca`,v0.18.0)上编出的二进制,同一个 zip 连跑两遍 `--sarif`,`diff` 出来:

| zip 形状 | SARIF 里每次都变的 | 原因 |
|---|---|---|
| `SKILL.md` 在包根(扁平) | `properties.artifact` = `skill:aguard-inbox-<随机>`;macOS 上还有 `uri` = `aguard-inbox-<随机>/install.sh` 和 `partialFingerprints["aguard/v1"]`(实测 3 行变) | artifact 名取解压目录的 base;macOS 的 `$TMPDIR` 在 `/var → /private/var` 软链下,`detect.relPath` 只解析了 root 的软链,`Rel` 失败,退回"最后两段",把随机目录名带进证据路径,指纹又由证据路径算 |
| `myskill/SKILL.md`(套一层目录) | `properties.artifact` = `directory:aguard-inbox-<随机>`(实测 1 行变) | 同上,artifact 名 |

后果:

- **GitHub Code Scanning 用 `partialFingerprints` 跨次匹配告警**。CI 里每次 check 同一个 zip,指纹全变 → 每次开一批新告警、
  旧的全部"已修复";审过、驳回过的告警下一次又冒出来。`sarif.go` 自己的注释说指纹"刻意不含行号,免得重开已经审过的告警"——
  这里重开的原因比行号还粗。
- **不只 SARIF**。同一个随机名还出现在:`--json` 的 artifact `name`/`path`、`locations` 的 "Config root"、扁平包的证据 `file`(macOS);
  终端报告的 "skill aguard-inbox-<随机> — curl piped to shell";`scan` 的 Downloads 一节 JSON 里的证据 `file`(同样走 `ExtractZip`)。
- **`aguard approve x.zip` 记下的名字和路径是一个已经删掉的临时目录**:实测批准库里 `name: aguard-inbox-1441416832`、
  `path: /var/folders/…/T/aguard-inbox-1441416832`,终端提示 ``Run `aguard check "/var/folders/…/T/aguard-inbox-1441416832"` `` ——
  照做必然 "no such file"。批准的**键**(内容树哈希)不受影响,两次相同。
- **同一个根因的另一面:结果依赖包外的文件**。包里有 `plugins/installed_plugins.json` 时,`CollectTarget` 把解压目录当 root 走
  `CollectAll`,`home` = 解压目录的父目录 = **共享的 `$TMPDIR`**。实测:在 `$TMPDIR/.claude.json` 里放一个 MCP server,
  `check root.zip` 就多出一个 `mcp:planted` artifact 和它的 `EXEC-001`,`locations` 里还列出 `$TMPDIR/.claude.json` 等三处——那些文件根本不在包里。
  Linux 上 `$TMPDIR` 通常是全机共享的 `/tmp`,同机任何用户都能往别人的 zip 检查里加发现。

## 初步方向

解压到**私有临时目录里一个以包文件名命名的子目录**(`<MkdirTemp>/x.zip/`,临时根先解析软链),于是 artifact 名 = 包名、
证据路径 = 包内路径、`home` = 一个只装着这个子目录的私有空目录;`checkTarget` 和 Downloads 那一路查完后把指向临时目录的
`path`/`locations` 改写成包路径。目录目标的输出一个字节不变。
