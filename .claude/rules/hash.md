<!-- SPDX-License-Identifier: MIT -->
---
paths:
  - "internal/collect/**"
---
## Canonical 哈希(`internal/collect/hash.go`)

skill 与 plugin 用树哈希(按相对路径排序 + 每个文件的 sha256);单文件用 sha256。它是信誉库的 key,所以必须
跨机器、跨 checkout 稳定 —— 这正是 `collect.ExcludeFromHash`(`.git`、`node_modules`、`dist` …)
要同时从**哈希遍历和扫描遍历**中排除的原因。**改动哈希逻辑会让
[internal/reputation/data/reputation.json](../../internal/reputation/data/reputation.json)
里的每一条记录全部失效**,以及**闸门里每一条已存的用户批准**(`internal/gate` 也按这个哈希建索引),
须用 `aguard hash` 重新生成。`TestHashGolden` 用两个字面常量钉住了当前定义 —— 现有的其他哈希测试
全都是"拿一个哈希和另一个同样算法算出来的哈希比",定义整体挪动时它们照样绿,只有字面常量会红。
`test/` 故意**不**排除 —— payload 会藏在那里。

**读文件必须流式,不能整读**(`sumFile`)。被哈希的字节是 artifact 作者选的,而 `os.ReadFile`
会照着那个文件的大小去申请内存 —— 往 skill 目录里丢一个多 GB 的文件,就能让扫描器 OOM 而不是给出
判决;**被扫对象能干掉扫描器的工具不是一道控制**。同一个问题有个更安静的孪生兄弟:`os.Open` 打开
一个 FIFO 会一直阻塞,所以 `sumFile` 先 `Stat` 掉非常规文件再开。**同一个守卫在内容侧是
`detect.regularFile`,而"上限由读强制"在内容侧是 `readCapped` 的 `io.LimitReader`** —— 这里修好了
不等于那边修好了:两条路各自开文件,而当年只修了这一条,于是一根管子照样能挂死 `scan`/`check`
和闸门,一个边读边长的文件照样能把 1 MiB 的帽子顶掉。`TestHashLargeFileStreams` 断言的是**分配的字节数**
而不是耗时或哈希值 —— 那才是退回整读时会红的那条性质。

