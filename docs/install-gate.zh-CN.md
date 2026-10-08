<!-- SPDX-License-Identifier: MIT -->
# 加载时闸门

`aguard hook` 在 **agent 加载一个 skill 之前**审它，凡是带发现的都会先问你一句。用的是
`aguard check` 不带 `--llm` 时那套静态扫描 —— 不调模型、不联网、不执行任何东西 —— 只是把答案送到它真正值钱的
那一刻。

英文版：[install-gate.md](install-gate.md)。

## 安装

```bash
aguard hook install             # 合并进 ~/.claude/settings.json，先自动备份
aguard hook install --dry-run   # 只看改动，不写
aguard hook uninstall           # 只删它自己那几条
```

装完重启 Claude Code —— hook 是会话启动时读的。

安装是**合并不是覆盖**：不认识的顶层键、你的 permissions、你自己手写的 hook 全部原样保留，重复跑
是空操作。三个事件共用**一条命令**（runner 自己按 `hook_event_name` 分派），所以 settings 里不会
出现任何 shell 管道 —— 顺带也意味着这个闸门的安装动作不会被 `HOOK-001` 报出来。

## 它做什么

| 事件 | 行为 |
|---|---|
| `PreToolUse[Skill]` | 解析 skill、扫描、判断。已批准 → 完全沉默。干净且第一次见 → 一行提示，随后记住。**低于阈值但有 medium 及以上发现 → 每次加载都一行提示，不记住**（直到内容干净，或你 `aguard approve` 它）。发现达到阈值 → **拦住加载并问你**。 |
| `PostToolUse[Skill]` | 你在弹窗里同意了就记下来 —— 但要先重新读一遍目标，确认哈希仍是弹窗里给你看的那份。 |
| `SessionStart` | 盘点整个 root，报告未批准且带发现的 artifact。**只告知，拦不住**。没有要报的东西时，它仍然会说明闸门**管不着**哪些面 —— 一个安静的环境，恰恰是主人最容易认为"全都被守住了"的那种。 |

装完想确认它真的在工作：

```bash
aguard hook status
```

三种状态说得很清楚：**已激活** / **没装** / **注册了但已死**。第三种是这条命令存在的理由 ——
注册进 settings.json 的是**绝对路径**，而路径会失效（二进制搬了、构建目录被清了、dotfiles 同步到
一台从没装过的机器）。那时候 Claude Code 每次都去执行一个不存在的命令，**每个 skill 都无审计放行，
而那份安静跟"一切正常"长得一模一样**。退出码：激活 `0`，其余 `1`。

**`aguard scan` 也会报这一种**（`GATE-001`）—— 没人会定期跑一个状态命令，而 `scan` 是大家本来
就在跑的。它是维度 0 的 note，**不计分**：死掉的 hook 让*报告*不可信，不让任何 artifact 更危险。

## 为什么是加载时，不是安装时

Claude Code 一共九个 hook 事件，**没有任何一个在插件/skill 安装时触发**。就算有，也做不全：一个
skill 还可以是 `git clone` 进来的、`cp` 进来的、或者你在某个会话根本看不见的终端里手动放进去的。

**加载是唯一守得住的边界，而且它就是那条要紧的边界。磁盘上的 skill 是惰性的** —— 在 agent 把它读
进上下文之前，它什么也做不了。这跟 `collect/unowned.go` 依据的是同一条道理：决定一样东西危不危险
的，是 agent 有没有加载路径进得去，而不是那些字节在不在。

所以这个闸门不去拦下载，它拦的是**这个 artifact 开口说话**。

## 批准记的是内容，不是名字

```bash
aguard approve ./some-skill        # 信任这一份确切的字节
aguard approvals                   # 列出已信任的
aguard approvals forget <hash>     # 撤销一条;前缀也行,包括闸门消息里打印的短哈希
aguard approvals forget all
```

key 是 artifact 的 **canonical 哈希** —— 就是信誉库用的那个树哈希，跨机器、跨 checkout 稳定。批准
`pdf-export` 不等于批准"从今往后所有叫 pdf-export 的东西"，批准的是那些字节。作者推个更新、你重装
一次、或者有人改掉一个字符，哈希就变了，闸门**自己**会重新问：没有过期时间要调，没有缓存要失效，
也没有什么命令需要你记得重跑。

没有哈希的内容批准不了。解析失败的配置文件、扫描器打不开的文件，都没有可以拿来做批准 key 的哈希，
所以 `aguard approve` 会拒绝它 —— 说明是哪个 artifact、为什么，退出码 2，状态文件不动。
`aguard check <path>` 会告诉你哪里没读到。

状态文件是 `~/.claude/.aguard-approvals.json`，权限 0600，原子写入。除了批准，它还存着每个仍在等你
回答的弹窗背后的判决（最多一小时）：你的回答是在另一次 hook 调用里到达的，而这个文件是两次调用之间
唯一共享的东西。**读不出来的状态文件会退化成
"什么都问一遍"，而不是"什么都放行"** —— 这两个失败方向不对称：丢批准只是多弹几次窗，而信任一个读
不懂的文件是白送一次静默放行。同理，`aguard hook` 绝不覆盖一个它没读懂的文件。

## 你的权限模式会改变它的行为

Claude Code 的 `auto` / `acceptEdits` / `bypassPermissions` / `dontAsk` 这几个模式**会自动答应
弹窗**。在这些模式下返回 `ask` 不是"问你",是"替你答应了",而且**你什么都看不到**。

所以闸门会读事件里的 `permission_mode`:发现这个模式不会真的问人时,**把 `ask` 改判成 `deny`**,
并在理由里点名是哪个模式 —— 你得知道为什么没被问,否则闸门看起来比你配置的更严,而你要改的那个
设置根本不在闸门这边。

```
This session runs in permission mode "auto", which answers prompts automatically.
An "ask" would have been accepted without you ever seeing it, so this was REFUSED instead.
To load it anyway, decide outside the session: aguard approve "<path>"
```

`default` 和 `plan` 不受影响(那两个会真的弹给人看);**不认识的新模式也不升级** —— 把 Claude Code
未来的每次改版都变成一堵拒绝墙,是另一种把自己搞到被卸载的方式。

这条是在真机上找出来的:一个 51/100、带完整凭证外泄链的 skill 返回了 `ask`,会话自动接受,skill
照常加载,**全程没有任何显示**。一个决定被吞掉的闸门是在无声地 fail open,而这正是本包唯一不允许
自己出现的失败形态(见下)。

## 它覆盖不到什么

明说，因为一个你以为全覆盖的闸门，比一个你知道边界在哪的闸门更危险。

- **只有 skill 走加载时闸门。** 插件自带的 hook 和 MCP server 从会话第一轮就是活的 —— 根本没有
  "加载"那一刻可以拦。`CLAUDE.md` 同理。这些由 `SessionStart` 那次盘点覆盖，而它**只能告诉你**。
- **hook 传过来的是 skill 名字，不是路径**，所以闸门要自己再查一遍（项目 → 用户 root → 已装插件）。
  查不到、或者两个插件都提供同名 skill，一律报 **`GATE-000`：未经审计就加载了** —— 绝不当成干净。
- **它 fail-open，但很大声。** 内部错误、名字解析不了、状态文件写不进去，都会放行**并说明**。一个
  自己扫描器一坏就把编辑器锁死的安全工具会被卸载，卸掉之后它什么也保护不了。它唯一不许做的是沉默：
  "我没审到"和"我审了没问题"是这整个工具存在的意义所在的那两个答案。
- **干净路径那行提示可能看不见。** 它走的是 hook 协议的 `systemMessage` 字段,实测在
  VSCode 扩展里不渲染。这不影响拦截(拦截走的是 `permissionDecision`,一定生效),但意味着
  "审过了、没问题"这件事你可能收不到反馈。想确认就跑 `aguard hook status` 和 `aguard approvals`
  —— 后者会列出它审过并信任的每一份内容。
- **静态方法的限制照旧**：README 里那份能力边界在这里全部成立 —— 闸门证明不了恶意、解不出混淆
  payload 的意图、抓不到零日。

## 配置

两个旋钮管的都是**噪音，不是能力**；它们都不能让闸门去调模型或联网。

```yaml
gate:
  fail_on: high     # low | medium | high | critical —— 与 `check --fail-on` 默认值相同
  action: ask       # ask（默认）—— 弹窗交给你决定
                    # deny        —— 直接拒绝，给没人守着的机器用
```

## 手动注册

```json
{
  "hooks": {
    "PreToolUse":  [{"matcher": "Skill", "hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}],
    "PostToolUse": [{"matcher": "Skill", "hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "/path/to/aguard hook"}]}]
  }
}
```

用**绝对路径**。编辑器的 `PATH` 不是你 shell 的 `PATH`，而一个找不到的 hook 是静默失败的 ——
那等于闸门根本不在。
