<!-- SPDX-License-Identifier: MIT -->
# 007 — EXEC/OBF 维度用正则替身，精度受限

- **类别**：检测精度
- **严重程度**：中
- **状态**：进行中（ROADMAP **A.1**，`Now` 的唯一一项）
- **2026-09-14 更新**：AST 降为**条件项** —— "正则精度是不是真问题，只有 benchmark 的『错误响』
  那一列能回答"。所以这条 issue 的结论（精度受限）仍然成立，但"要不要上 AST"要看语料 benchmark
  （`baselines/results/`）里误报按规则的分布。第一个实证：一条 `INJ-001` 正则把"教 agent 拒绝注入"的
  防御性文字判成攻击（后由 hard-negative 样本钉住）。

## 问题描述

代码执行（EXEC）与混淆（OBF）两个维度目前用正则作为**替身实现**（regex stand-in）。
正则在这两个维度上同时吃两头亏：

- **漏报**：任何打断字面量的手法都能绕过——见 [001](001-line-continuation-evasion.md)
  （续行）与 [002](002-unicode-evasion.md)（零宽/同形字）。这两个已确认绕过的
  **共同根因**就是"匹配字节而非匹配语法"。
- **误报**：注释里、字符串里、文档示例里的 `eval` / `curl | bash` 与真实调用无法区分。
  项目已经为此单独做了注释识别（[internal/detect/comments.go](../internal/detect/comments.go)）
  和声誉允许列表（可信工具的示例不再淹没报告），这些都是在给正则打补丁。

## 出处

- ROADMAP "Now (in progress)"：`A.1 AST detection (pure-Go, no CGO) — raise precision on
  EXEC/OBF over the regex stand-in`
- 现有规则实现：[internal/detect/rules.go](../internal/detect/rules.go)、
  [internal/detect/rules_data.go](../internal/detect/rules_data.go)

## 约束（不可妥协）

- **纯 Go、`CGO_ENABLED=0`**：单二进制分发是项目的核心卖点，不能引入需要 CGO 的解析器。
- **确定性**：AST 结论进入 `overall`，必须可复现、与并发顺序无关（现有引擎已保证
  按索引写回结果）。
- **只读不执行**：解析绝不等于求值。

## 修复方向

1. 分语言落地，优先级按风险：**shell**（hook / 脚本，风险最高）→ **JS/TS**
   （skill 脚本常见）→ **Python**。Go 生态有纯 Go 的 shell 解析器
   （如 `mvdan.cc/sh/syntax`）与 JS 解析器可选，需评估依赖体积与许可。
2. **渐进替换而非一次性切换**：AST 命中与正则命中并存一段时间，用现有测试与
   对抗语料对比两者差异，确认 AST 是超集后再下线对应正则。
3. 落地时**一并关闭 001/002**：AST 天然不受续行影响；归一化则在词法阶段处理。
4. 同步翻转对抗语料中 001/002 的反向断言。

## 关联

- [001](001-line-continuation-evasion.md)、[002](002-unicode-evasion.md)（本条是两者的根治方案）
