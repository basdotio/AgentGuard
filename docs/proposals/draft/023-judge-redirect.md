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
