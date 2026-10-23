<!-- SPDX-License-Identifier: MIT -->
# 028 — 服务器趟的 remote-execution 在"工具自己声明了执行"时降档:实测否决,不要再论证

- **类别**:检测精度(已否决)
- **严重程度**:—
- **状态**:**不规划**。2026-10-23,P-098/P-099 之后清点良性服务器上判官剩下的误报时量过,未写代码。

> **这条 issue 的用途是防止重新发明。** `LLM-010` 问的是"服务器做了工具描述没说的事";一个叫 `run_command`、
> docstring 写着 "Execute a shell command" 的工具执行了 shell 命令,看上去正是"说了",于是"引文所在函数的名字或
> docstring 首行里有 execute/run/shell/command/eval 就把 `remote-execution` 降成 medium"听起来顺理成章。它不成立。

## 1. 出发点

P-098 之后,543 个良性服务器树(P-059 的分母)上判官单独报 high 的还有 32 个,其中 `remote-execution` 11 个,多数是
名字就叫 `run_command`、`execute`、`eval_wolfram`、`tool_run_kubectl` 的工具。

## 2. 为什么不成立

Cisco 的恶意服务器评测集里,一大类恶意样本本来就把危险写在明面上——函数叫 `execute_shell_command`、`evaluate`、
`import_and_execute`,docstring 写着 "Import and execute arbitrary functions"、"Unrestricted eval allows arbitrary code
execution"——然后真的执行任意输入。"声明了"不等于"安全",对这些样本降档就是漏报。

离线在 P-098 最终版的实测运行上模拟(引文所在 Python 函数的名字或 docstring 首行匹配
`exec|run|shell|command|eval|kubectl|script|terminal|bash|code`,且该样本的每条 high 都是这样的 `remote-execution`):

| 集合 | 带 `remote-execution` high 的样本 | 会因此不再被报 high |
|---|---:|---:|
| Cisco 恶意服务器(127) | 40 | **10** |
| 良性服务器树(543) | 14 | 2 |
| 其余恶意 MCP 样本(12) | 0 | 0 |

丢 10 个恶意换 2 个良性。

## 3. 复现

运行目录不在仓库里;复现用 `results/aguard/2026-10-23-llm-gpt-4.1-mini-p098-{cisco-servers,benign-servers}-branch-s3/`
的 `judge.jsonl` 定位样本,原始输出在 v0.31.0 的 `aguard-judge-raw-2026-10-p098.tar.gz`,按上面的判据对每条
`LLM-010` 的引文回到语料文件里找所在函数。

## 4. 什么时候值得重看

判官换了能区分"受控执行"(沙箱、白名单命令)和"执行任意输入"的问法之后;那是提示词的问题,不是严重度表的问题。
