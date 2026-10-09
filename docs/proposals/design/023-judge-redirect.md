<!-- SPDX-License-Identifier: MIT -->
# 023 — 判官端点回一个重定向,API key 就用明文发出去,或者被扫内容的摘录发给一台用户从没配置过的主机

- **来源**:P-003 在「不做什么」里记下的后续 ——"不做重定向处理(`CheckRedirect` / 跨主机重定向带走 Bearer 头)"
  ([complete/003-zero-dial-test.md](../complete/003-zero-dial-test.md))
- **依赖**:无(P-003 已合入)
- **分支**:`p/023-judge-redirect`

<!-- 没有「状态」行:文件所在目录就是状态(draft/ design/ complete/ rejected/),见 README.md。 -->

## 问题

`CheckEndpoint`(`internal/config/config.go`)拒绝非 https 的远程 `llm.base_url`,理由写在它自己的报错里:API key 是
Bearer 头、摘录是请求体,明文 http 会让两样都过网。setup、`llm test`、判官三处都过它。**但它只看配置里写的那个地址。**
判官的 client(`internal/judge/openai.go` `NewHTTP`,`&http.Client{Transport: Transport}`)用的是 Go 默认的重定向策略:
端点回一个 30x,client 照着 `Location` 再发一次,**不再过 `CheckEndpoint`**,报告里也一个字都没有。

在本仓库(go1.23.5)用 httptest 实测,`NewHTTP(…, nil)` 经 `judge.Transport` 接缝接到本机的几台 server 上(拨号按主机名路由,
不出网),`Judge` 发一个带标记的摘录,端点回重定向,看**目标**收到什么:

| 配置的端点 | 重定向到 | 301 / 302 / 303(改成 GET、丢 body) | 307 / 308(原样重发 POST) |
|---|---|---|---|
| `https://example.com/v1` | `http://example.com/…`(同主机、降成明文) | **key 明文发出** | **key 和摘录都明文发出** |
| 同上 | `https://collector.test/…`(别的主机) | key 被去掉 | key 被去掉,**摘录发给了它** |
| 同上 | `http://collector.test/…`(别的主机、明文) | key 被去掉 | key 被去掉,**摘录明文发给了它** |
| 同上 | `https://eu.example.com/…`(子域) | **key 发出** | **key 和摘录都发出** |
| 同上 | `https://example.com/…/`(同源,只差路径) | key 发回同一个源 | key 和摘录发回同一个源 |
| `http://localhost:11434/v1`(本机) | `http://collector.test/…`(远程) | key 被去掉 | key 被去掉,**摘录明文过网** |
| 同上 | `http://localhost:8080/…`(同主机、另一端口) | key 发出 | key 和摘录都发出 |

- **同主机降成 http,key 一定明文发出**:Go 判断"要不要带 `Authorization`"只比主机名、不比 scheme 和端口
  (子域也算同一个),所以 `https://h` → `http://h` 照带不误。这正是 `CheckEndpoint` 拒绝的那件事,只是换成由端点来说。
- **换主机时 key 被去掉了,摘录没有**:307/308 按规范原样重发请求体,而请求体就是脱敏过的摘录(不变量 #3 的
  "尽力而为"那一份)。收到它的是一台用户从没写进配置、`CheckEndpoint` 从没看过的主机。从本机端点跳出去的那一行,
  摘录还是明文过网 —— `CheckEndpoint` 放行 http 的唯一理由"不过线"在这里不成立。
- **全程没有报错**:35 次调用(5 种状态码 × 7 种目标)`Judge` 全部返回 `err == nil`,目标回的 200 被当成判决收下;
  `scan --llm` 的报告里没有任何一条说"判官的请求被转去了别处"。不变量 #5("任何遗漏都不许静默")管的是没看到的东西,
  这里是**发到了没说过的地方**,同样不该静默。
- 已知的 Go 缺陷 CVE-2024-45336(`a.com` → `b.com/1` → `b.com/2` 时把 `Authorization` 又带回来)在 go1.23.5 上实测已修:
  两跳都不带 key。所以问题不在 Go 的这层过滤,而在**判官根本不该跟着一个跨源的重定向走**。

## 初步方向

给 `NewHTTP` 造的那个 client 加一条 `CheckRedirect`:只跟**同源**(scheme 与 host:port 都和配置的端点一样)的重定向,
其余一律拒绝、不发出那一跳;拒绝不是崩溃,而是这次调用失败,经现有的 `LLM-000` 路径("LLM judge failed on N call(s) …")
进报告,报错里写明被转去的 scheme://host,静态结果一字不动,也不重试(那是一个确定的回答,不是抖动)。
P-003 的源码检查 `TestZeroDial_NoClientOutsideTheJudge` 描述的是 `http.Client{Transport: Transport}` 这个字面量,多一个字段
要让它、不变量 #1 和 spec §16.4/§13 的措辞一起跟上,让"只有一个 client、在 `NewHTTP` 里造、transport 就是接缝"仍然名副其实。

## 完成的判据

- [ ] `TestNewHTTP_RefusesCrossOriginRedirects`(`internal/judge/redirect_test.go`,新):经 `NewHTTP(…, nil)` 和 `Transport` 接缝
  (按主机名路由到本机的 httptest,https 那几台用 httptest 自带的证书,不出网),问题表里六个跨源目标 × 301/302/303/307/308 共 30 行:
  **目标收到 0 个请求**;端点恰好收到 1 个;调用返回错误,`errors.As` 取得到重定向拒绝、`isRetryable` 为假;报错里写着状态码、
  目标的 scheme://host 和配置的源,**不带**目标的路径。今天 30 行全红(目标收到请求、`err == nil`)
- [ ] **反向断言**(同一文件):不重定向的 https 端点和今天一样 —— key 和摘录照发、判决照解析、接缝计数 1;同源重定向
  (307/308 到同源另一路径,301 改 GET)照跟,第二跳经同一个接缝、带着 key 和摘录到同一台 server(接缝计数 2);
  同源重定向环在第 10 跳停下(Go 默认的上限保留),不会一直跟到超时
- [ ] `TestE2E_JudgeRedirectIsRefusedAndReported`(`cmd/aguard/judge_redirect_test.go`,新):`scan --llm` 对一个回 307、
  `Location` 指向另一个源(本机另一端口 —— 主机名相同,所以 Go 今天连 key 一起带过去)的端点:目标收到 0 个请求;
  报告里有一条 `LLM-000`,`Why` 里写着重定向和目标;`JudgeSummary` 跑了、`Failed == Calls`、`Retries == 0`(`max_retries` 为 2);
  `Overall` 和每个 artifact 的静态发现与不带 `--llm` 的扫描逐条相同。今天红:目标收到 key 和摘录,报告里没有任何一条
- [ ] `TestLLMTest_RedirectIsRefused`(同上):`llm test` 对同一个端点失败,报错点名重定向和目标,目标收到 0 个请求。
  **反向断言**:`TestLLMCommands_SetupTestStatus` 不改一字仍绿 —— 不重定向的端点 `llm test` 照旧报 `OK ·`
- [ ] P-003 的 `TestZeroDial_OnlyTheJudgeConnects`、`TestZeroDial_ClaimsNameTheTest`、`TestZeroDial_NoClientOutsideTheJudge` 仍绿;
  源码检查的注释和报错、不变量 #1、spec §16.4/§13 写的是新字面量的形状,"判官包里只有 `NewHTTP` 那一个 client、transport 就是接缝"
  这句话仍然一字不差地成立
- [ ] 反向断言不改一字仍绿:`TestHTTPClient_RoundTripAndRedaction`、`TestHTTPClient_ClassifiesRetryable`、`TestRun_RetriesOnlyRetryableErrors`、
  `TestCheckEndpoint`、`TestLLMSetup_KeyRoutesAndCleartextRefusal`、现有 `TestE2E_*`
- [ ] `make verify` 绿;`go.mod` 第二行仍是 `go 1.23.5`,`go version` 无工具链切换

## 不做什么

- **不改 `CheckEndpoint`**:配置那一头的规则(远程必须 https、本机可以 http)一个字不动;本条只管端点回的 30x
- **不改 `http.Client` 的 `Transport`、`Timeout`、`Jar`**:只加一个 `CheckRedirect`。接缝、"默认 client 不带超时"都不动
- **不动调用方自带的 client**:`NewHTTP` 收到非 nil 的 client 时原样使用(P-003 的 `TestNewHTTP_TransportSeam` 钉着);
  产品代码造不出自己的 client(源码检查),所以这只影响测试
- **不改重试策略**:429/5xx/传输错误照旧重试;只让"重定向被拒"不进重试。重定向环的 10 跳上限照旧按传输错误处理(和今天一样)
- **不让判官在第一次被拒后提前收工**:和 401 一样,每次调用各自失败,汇成一条 `LLM-000`
- **不加规则 ID、不改报告格式**:复用 `LLM-000` 和它现成的那句 "LLM judge failed on N call(s) …"
- **不碰 `internal/collect`、`internal/detect`、`internal/gate`、`internal/score`、`internal/report`**;不加依赖,不碰 `go.mod`/`go.sum`
- **不改代理行为**:`http.DefaultTransport` 照旧读 `HTTPS_PROXY` 等环境变量 —— 那是用户自己的设置

## 不能说什么

- **不说"判官只连配置的那台主机"**。能说的是:**判官不跟跨源的重定向**(scheme、主机名、端口任一不同即拒)。
  同源重定向照跟;DNS 把那个名字解析到哪、环境变量里的代理把请求转去哪,不归这一条管
- **不说"key 绝不明文发出"**:本条堵的是重定向这一条路;本机端点(`http://localhost…`)本来就是明文,`CheckEndpoint` 放行它的
  理由是不过线
- **不说 Go 的头过滤"已经够了"**,也不反过来说 Go 有漏洞:Go 按文档行为去掉跨主机的 `Authorization`,只是它比的是主机名
  (不比 scheme 和端口、子域算同一家),而且它从不去掉请求体 —— 问题表就是这两点
- **不把被拒的重定向说成"端点是恶意的"**:报错是中性的 —— 没跟、什么都没发过去、如果那才是真端点就把 `llm.base_url` 改成它
- 不说"所有重定向都被拒":同源的照跟

## 工作项

| W | 一句话 | 提交信息(不写 sha,rebase 会改) |
|---|---|---|
| 1 | 判官包与 cmd 的新测试,跑红(目标收到请求、`err == nil`、报告里没有 `LLM-000`) | `judge, cmd: tests — a redirect from the judge's endpoint is followed to a plaintext or unconfigured origin, and nothing says so (P-023)` |
| 2 | `NewHTTP` 的 client 加同源重定向策略,拒绝是不重试的调用失败 | `judge: the client follows a redirect only within the configured origin; any other is refused before the hop is sent, and not retried (P-023)` |
| 3 | 源码检查的注释与报错、不变量 #1、spec §16.4/§13 跟上新字面量的形状,并写明判官不跟跨源重定向 | `cmd, rules, spec: the zero-dial source check names the seam client with its redirect policy, and invariant #1 says the judge does not follow a redirect out of its origin (P-023)` |
| 4 | `judge.md`、`docs/llm-judge*.md`、spec §11 写上重定向那一句 | `docs, rules: the judge follows a redirect only within the configured origin (P-023)` |
| 5 | 本文件「完成」、索引 | `proposals: P-023 (P-023)` |

## 未决问题

1. **全拒,还是只拒跨源?**
   **建议**:只拒跨源。同源重定向(只差路径)不把任何东西送到新地方:key 和摘录去的还是用户配置、`CheckEndpoint` 放行过的那个源。
   全拒会让"端点把路径规范化一下"这种无害行为变成判官失败,这超出了本条要解决的两件事(明文、没配置过的主机)。
   **已决(2026-10-09)**:按建议。
2. **"同源"怎么比?和谁比?**
   **建议**:scheme 小写、主机名小写、端口(没写的按 scheme 补成 443/80)三样都相等才算;比的是**配置的端点**那一跳(`via[0]`)。
   (实现时更正:设计时这里写过"不和上一跳比,否则能一步步走远",那是错的 —— 同源是等价关系,和上一跳比结果完全一样;
   选 `via[0]` 只是让"配置的源"在代码里直接可见。)端口要比:同一台主机另一个端口上的可以是另一个服务,
   而 Go 判断带不带 key 时不看端口(问题表最后一行)。
   **已决(2026-10-09)**:按建议。
3. **被拒的重定向要不要重试?**
   **建议**:不重试。那是端点给出的确定回答,不是抖动;重试只会让同一个端点再收几份一样的摘录,再被拒几次。
   **已决(2026-10-09)**:按建议。
4. **报错里写不写重定向的目标?写多少?**
   **建议**:写状态码、目标的 scheme://host[:port](用 `%q` 引起来:`Location` 是端点写的,引号让控制字符、方向字符显形)、配置的源,
   **不写路径和 query**(签名 URL 之类会把一次性凭据放在 query 里)。运维要的是"被转去了哪里",以便决定要不要改 `llm.base_url`。
   **已决(2026-10-09)**:按建议。
5. **自己写 `CheckRedirect` 会替掉 Go 默认的"10 跳即停",要不要保留?**
   **建议**:保留,同样 10 跳、同样的报错原文,按今天的方式(传输错误)处理;不然一个同源的重定向环会一直跟到单次调用超时。
   **已决(2026-10-09)**:按建议。
6. **P-003 的源码检查要不要顺带钉住"`NewHTTP` 的字面量必须带 `CheckRedirect`"?**
   **建议**:不。实测 `isSeamClientLiteral` 本来就放行带键的其他字段(它的注释原话是"重定向策略、超时可以加进来,它们都不带
   transport"),加上 `CheckRedirect` 之后检查照绿;错的只是它的注释、报错和不变量 #1 / spec 里写的字面量形状。源码检查管的是
   "有没有绕开两个计数器拨号",重定向策略由行为测试经 `NewHTTP(…, nil)` 钉住;把两件事绑进一条检查,改哪一边都要先读懂另一边。
   所以只改措辞,并写明:`CheckRedirect` 只决定下一跳发不发,放行的每一跳都过同一个 `Transport`,计数器照样数得到。
   **已决(2026-10-09)**:按建议。
