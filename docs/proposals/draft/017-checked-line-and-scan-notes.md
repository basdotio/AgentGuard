<!-- SPDX-License-Identifier: MIT -->
# 017 — 摘要自相矛盾:check 一个文件一边列发现一边说 "Nothing was found to check";已加载内容里没读到的部分不动 "looks safe"

- **来源**:P-013 记下的后续(它的「不做什么」(a)(b)、「不能说什么」最后一条、未决问题 8;人定 2026-10-09 合成一份,P-013 合入后另开)
- **依赖**:P-013(已合入 `main`)
- **分支**:`p/017-checked-line-and-scan-notes`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

报告的 Summary 是不写代码的读者真正会读的那一半,它有两句:头条(等级档 + 计数派生的一句话)和 Checked 那句(检查了什么)。
在本仓 `main`(`fd28344`)编出的二进制上实测,两句都会和**同一份报告**下面列出的东西打架。夹具全部现搭在临时目录里。

**1. Checked 那句只从采集器的清单计数(`EnvSummary`)推,而有几类被扫过的东西不在清单里。** 于是:

| 目标 | 报告里同时出现的 | Summary 的第二句 |
|---|---|---|
| `check install.sh`(`curl … \| bash`) | `EXEC-001` high,"There are problems you should fix…" | `Nothing was found to check under this root.` |
| `check hello.sh`(干净) | "Your Claude Code setup looks safe." | `Nothing was found to check under this root.` |
| `check tool/`(普通目录,带同一个 `install.sh`) | `EXEC-001` high | `Nothing was found to check under this root.` |
| `scan --root` 一个只有 `CLAUDE.md` 的 `.claude`(带 `curl … \| bash`) | `EXEC-001`,69/100 Elevated | `Nothing was found to check under this root.` |
| `scan --root` 一个只有 `settings.json`、里面只有 `env` 块的 `.claude` | 一个 `permission:settings env` artifact | `Nothing was found to check under this root.` |
| `scan --root` 一个 `settings.json` 是 `chmod 000` 的 `.claude` | 头条已对冲(P-013),Not checked 里有 `IO-000` 指着 `settings.json` | `Nothing was found to check under this root.` |

终端默认、`--verbose`、`--md`、HTML(`scan --html`)四个渲染器一字不差。原因:`check <单文件>` 产出 `instruction`(或 `command`)类
artifact、`check <普通目录>` 产出 `directory` 类 artifact,`CollectTarget` 给这两种的 `EnvSummary` 都是空的;root 里的
`CLAUDE.md` 是 `instruction` 类,`settings.json` 的 `env` 块是不计条数的 `permission` 类 —— 清单里都没有它们,于是
`checkedParts` 什么都数不出来,`checkedWithGaps` 落到 "Nothing was found"。读不了的 `settings.json` 是扫描级 `IO-000`、
没有 artifact,P-013 的"找到了、没检查全"只点名挂在 artifact 上的 note,所以它也落到同一句。

一句"什么都没查"紧挨着它自己查出来的 high,读者只能二选一地不信其中一句。

**2. 头条只在"挂在 artifact 上的覆盖 note,或任何位置的 `IO-000` / `PARSE-000`"时对冲(P-013 人定的集合),而 detect 关于已加载
内容的覆盖 note 全部是扫描级的 `COV-000`**(`detect.Engine.Run` 把每个 artifact 的维度 0 结果收进扫描级 notes)。实测:

| 夹具 | 没读到的 | 分数 / 头条 |
|---|---|---|
| skill 的 `SKILL.md` 写 "Run sh sub/inner.sh",`sub/` 是 `chmod 0111`,里面是 `curl … \| bash` | `COV-000` medium "Entries in this artifact could not be read" | 100 / "Your Claude Code setup looks safe." |
| skill 里一个 1.1 MB 的 `big.sh`,末尾是 `curl … \| bash` | `COV-000` "File too large, content scan skipped" | 100 / "looks safe" |
| skill 的 `SKILL.md` 写 "Run node node_modules/dep/setup.js" | `SUP-004` medium(指向扫描不读的目录)+ `COV-000` "Third-party / VCS trees not read" | 88 / "looks safe. 1 finding needs a look" |
| `settings.json` 注册的 hook 跑 `sh ~/.claude/hooks/missing.sh` | `COV-000` "Hook script not followed" | 100 / "looks safe" |
| `.claude/rules` 是指向 root 外的软链(dotfiles 常见布局) | `COV-000` medium "Entries resolve outside the scanned root, not read"(Why:"They are loaded by Claude Code but were NOT scanned") | 100 / "looks safe" + "Nothing was found to check" |

前两行就是 W-001 / 超大文件这两种规避形状:payload 放在扫描器读不到的地方,报告的头条照样说安全,只有折起来的
"Not checked — 1 coverage note(s), highest medium [COV-000]" 那一行知道。四个渲染器的头条相同。不变量 #5 要求的是"缺口不静默",
这些 note 确实进了 Not checked;但 P-013 已经把"looks safe"定义成"Claude Code 加载的东西都读全了"的断言,而上面每一行都是
加载的东西没读全 —— 头条说错了话。

`collect` 也有几条同类的扫描级 `COV-000`(加载命名空间里解析不了的条目、`rules/` 等解析到 root 外、目录深度上限、托管策略
指令文件、隔离区越界、`@import` 越界 / 拒读凭据 / 深度上限、桌面版会话缓存上限),同样不动头条。

## 初步方向

只改 `internal/report` 的白话层(`plain.go`)和三个人读渲染器调用它的地方,从 `ScanResult` 里已有的数据派生,不加阈值、
不挪数据:Checked 那句在清单数不出东西时改数实际扫过的 artifact,并把扫描级 `IO-000` / `PARSE-000` 也点名成"没检查全";
头条的集合扩到"已加载内容没读全"的扫描级覆盖 note,按设计不读的几条照旧不动头条。JSON / SARIF、分数、退出码不动。
