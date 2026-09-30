<!-- SPDX-License-Identifier: MIT -->
# 003 — 生成/vendor 目录内容不扫描

- **类别**：覆盖缺口
- **严重程度**：中（内容确实未读，但"指向未读目录"这一半已被拦截）
- **状态**：**已修复**（拆成两个谓词；第三方树刻意不读是设计决定）

## 问题描述

`dist/` `build/` `out/` `node_modules/` `vendor/` `coverage/` 这些目录的**内容从不被读取**，
所以任何规则都不会在里面命中。攻击者把 payload 放进 `dist/setup.sh` 即可让规则集完全失效。

排除是有原因的、不是疏漏：这些目录被排除在**规范哈希**之外，才能让一个产物的身份在
重新构建后保持稳定——而那个哈希是声誉库（reputation）的键。读它们和哈希稳定性是
一对需要分开处理的目标。

## 已有缓解

- 产物**指向**被排除目录（例如 `SKILL.md` 里写 "run `dist/setup.sh`"）会被报为
  `SUP-004`（维度 5，medium，**参与打分与门禁**）——可读的那一半把 agent 指向了
  被名字变得不可读的那一半。
- 没有被任何产物引用的跳过目录，产生一条聚合的 `COV-000` 披露（每次扫描一条）。

## 出处

- 跳过名单：[internal/collect/skip.go:13](../internal/collect/skip.go)（`GeneratedDir`）
- 对抗语料反向断言：[cmd/aguard/adversarial_test.go:153](../cmd/aguard/adversarial_test.go)
- README "Coverage caveats" + ROADMAP "Known limitations"

## 修复方向

1. **扫描侧与哈希侧解耦**：读取这些目录用于检测，但仍把它们排除在规范哈希之外。
   现在两件事共用一个 `IsGeneratedDir` 判断，需要拆成两个谓词（`ExcludeFromHash`
   / `ExcludeFromScan`）。
2. **只开该开的**：`dist/` `build/` `out/` 是**本产物**构建产物，值得读；
   `node_modules/` `vendor/` 是第三方树，其 finding 说明不了产物本身的问题，
   读了只会制造噪音——ROADMAP 已有此判断，建议保持。
3. 注意体积：`dist/` 可能含大文件/压缩产物，需要沿用现有的读取上限与 `COV-000` 通报。

## 关联

- [004](004-unknown-extension-unscanned.md)（同为"读不到"类缺口的另一条路径）

## 修复记录

采用了"修复方向 1"：`internal/collect/skip.go` 把**一个**谓词拆成**两个**，因为它一直在回答
两个不同的问题：

```
"这个目录属于产物的身份吗？"   → 哈希
"这个目录的内容该被读吗？"     → 扫描
```

**不对称是重点**：

- `ExcludeFromHash` **一字未改**，而且必须保持不动。规范哈希是声誉库的键，必须在两台机器上、
  重新构建之后都相同。`.git/index`、`FETCH_HEAD`、reflog 带每次 checkout 的状态；
  `node_modules/` 随 lockfile 解析和平台变。**往这里加一个目录就是对所有已存哈希的破坏性变更。**
- `ExcludeFromScan` **更小**，因为"读"的代价只是噪音。移出去的是产物**自己的**构建产物
  （`dist/` `build/` `out/` `.next`）——它由同一棵树里的代码生成，正是"按名字跳过"给 payload
  留的藏身处。留在里面的是第三方树（`node_modules/` `vendor/`）：里面的 finding 说的是别人的
  依赖而不是这个产物，一个报它们的扫描器会教用户学会跳读。`.git/` 也留着——git 自带的
  `hooks/*.sample` 里有 `curl` 形状的示例，实测读它会产生一条关于示例的 high。

### 实测验证（哈希稳定性是最大风险）

```
初始 hash:        cccf295645f1c714…
改 dist/ 后:      cccf295645f1c714…  ✅ 不变（声誉键跨重建稳定）
改 node_modules:  cccf295645f1c714…  ✅ 不变
改 SKILL.md:      473e8b38975b687f…  ✅ 变了（真内容变化必须换身份）
```
而 `dist/setup.sh` 里的 `curl … | bash` 现在会被 **EXEC-001** 报出来。

### 调用点按"问题"分流

拆分的意义是**选择在调用处可见**：

| 调用点 | 用哪个 | 因为 |
|---|---|---|
| `collect/hash.go` | `ExcludedFromHash` | 算身份 |
| `detect/detect.go` 树扫描 | `ExcludedFromScan` | 决定读什么 |
| `collect/loaded.go` 产物发现 | `ExcludedFromScan` | 发现的东西会被扫 |
| `judge/excerpt.go`、`decode.go` | `ExcludedFromScan` | 摘录/解码也是读 |

历史名字 `IsGeneratedDir` 曾保留为 `ExcludedFromHash` 的别名——每个既有调用者当初的语义都是
"我在走一棵树来算或保留一个身份"。想要"读"那个问题的调用者必须显式写 `ExcludedFromScan`。
**别名于 P-038(2026-09-30)删除**:全仓零调用,两个真名已经说清了各自回答哪个问题。

### 语料断言拆成两条

原来那一条（payload 在 `dist/`）翻转为 `wantCaught: [EXEC-001, FS-003]`。新增一条针对
`node_modules/`，保持 `SUP-004` + 反向断言——**那不再是缺口而是设计决定**，措辞也改成了这个：
"第三方树刻意不扫（里面的 finding 说的是别人的依赖）；被抓住的是产物把 agent 指进去这件事"。

`COV-000` 的标题和文案也跟着改了（原来说"generated/vendored 目录被跳过"，现在只对第三方/VCS
树成立，并明确说产物自己的构建产物**是**被读的）。
