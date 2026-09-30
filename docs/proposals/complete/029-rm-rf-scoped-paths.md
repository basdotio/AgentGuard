<!-- SPDX-License-Identifier: MIT -->
# 029 — 清一个缓存目录也判"删整个家目录":FS-003 把起头是 `/` 或 `~` 的路径都当成根

- **来源**:plan §3.1 第 1 步剩余"FS-001/FS-003 先分析再定";P-028 设计阶段的 FS 测量;执行序 4 的后半
- **依赖**:无(P-028 已合入 dev)
- **分支**:`p/029-rm-rf-scoped-paths`

<!-- 目录即状态。 -->

## 问题

`FS-003`(high,维度 9)的标题是 "rm -rf against root/home",正则却是 `\brm\s+-rf\s+(/|~|\$HOME|\*)`:只看路径**第一个字符**。
于是任何**以 `/` 或 `~` 开头的具体子路径**都算"删根目录":

```
rm -rf ~/.npm/_npx                    # macOS 清理 skill,npx 缓存
rm -rf ~/Library/Caches/*             # 同上
rm -rf ~/.bun/install/cache           # bun 锁文件更新
rm -rf /var/lib/apt/lists/*           # Dockerfile 的标准写法,三个 skill 各一
rm -rf ~/.config/jira/.cache          # jira-cli
```

语料 3,220 个良性里它命中 10 个,其中 **7 个的 high 只有它一条**,都被闸门拦下。300 个恶意里它命中 **0** 个。

反过来,真正删整棵树的写法它漏了一种:`rm -rf "$HOME"`(引号里的 `$HOME`)。

## 初步方向

目标只认**整棵树的根**:`/`、`/*`、`~`、`~/`、`~/*`、`$HOME` / `${HOME}` / `"$HOME"`(可带 `/`、`/*`)、`*`,且后面紧跟词边界
(空白、行尾、引号、`;` `&` `|` `)`)。`~/.npm/_npx`、`/var/lib/apt/lists/*` 这类具体子路径不再命中。
顺带认同义的旗标拼法(`-fr`、`-Rf`、`-rfv`、`-r -f`、`--recursive --force`)与 `--no-preserve-root`,不然换个字母顺序就绕过。

**值得设计**:人 2026-09-29"开 P-029 做 FS-003"。

## 设计阶段的测量(语料 4f964622,在引擎**实际命中 FS-003 的行**上重算)

逐文件 grep 会高估(39 个良性):引擎对维度 9 丢掉纯注释行,也不在所有角色上跑。收窄只会减少命中,所以在
`make bench` 原始输出里 `FS-003` 的证据行上重算候选是忠实的:

| 变体 | 良性样本 | 其中 high 只有 FS-003 | 恶意 |
|---|---|---|---|
| 现状 | 10 | 7 | 0 |
| **只认整棵树的根** | **2** | **1** | 0 |

剩下 2 个是护栏类文字把 `rm -rf /` 当成**要拦的东西**列出来(`"- rm -rf / recursive deletes"`、一个 hook 的拦截清单 `"rm -rf /",`)。
是提及不是执行;为清单格式单独判断是拟合,接受。

## 完成的判据

- [ ] `TestDetect_RecursiveDeleteOfATreeRoot`(`internal/detect/rmrf_test.go`):
      - 必响 `FS-003`(high、维度 9):`rm -rf /`、`rm -rf /*`、`sudo rm -rf ~`、`rm -rf ~/`、`rm -rf ~/*`、`rm -rf $HOME`、`rm -rf "$HOME"`、
        `rm -rf ${HOME}/`、`rm -rf *`、`rm -fr /`、`rm -Rf ~`、`rm -rfv /`、`rm -r -f ~`、`rm --recursive --force /`、`rm -rf --no-preserve-root /`
      - **反向断言(必静)**:`rm -rf ~/.npm/_npx`、`rm -rf ~/Library/Caches/*`、`rm -rf /var/lib/apt/lists/*`、`rm -rf ~/.bun/install/cache`、
        `rm -rf ./dist`、`rm -rf node_modules`、`rm -rf "$HOME"/.cache/build`、`rm -rf /tmp/session-*`
- [ ] `make bench`:良性 **121 → 115**、恶意 **105 不变**、hard negative 4/19、3539 全 scored
- [ ] 真机 `scan --root ~/.claude`:`FS-003` 前后对比贴进未决问题;`make verify` 绿

## 不做什么

- **不碰 `FS-001`**:恶意侧命中 40,翻回良性 2 个不值这个风险
- **不猜意图**:`sg-rm-rf-no-confirm`(`rm -rf "$HOME"/.cache/*/build "$HOME"/work/*/dist …`)删的是家目录下的通配子路径,
  和良性清理脚本同形;它现在没有任何发现,本条也不去抓它
- **不改其他 `FS-*`、不改维度与定级**

## 不能说什么

- **不写"FS-003 覆盖危险删除"**:只覆盖删整棵树的根;删家目录下某个具体目录(`~/Documents`)不在内
- 数字带分母:"3,220 个良性里 10 → 2"

## 工作项

| W | 一句话 | 提交信息 |
|---|---|---|
| 1 | `rmrf_test.go`:必响、必静两组;先红 | `detect: tests for rm -rf of a tree root versus a scoped path — red until FS-003 reads the whole target (P-029)` |
| 2 | `FS-003` 目标改为整棵树的根 + 词边界,旗标认同义拼法 | `detect: FS-003 fires on deleting a whole tree, not on clearing a cache under it (P-029)` |
| 3 | `make docs`;`make bench` → `baselines/results/aguard/2026-09-29c/` | `baselines: aguard after FS-003 — benign 121 -> 115, malicious unchanged (P-029)` |

## 未决问题

人说"做 FS-003",下面**先按建议答案做**,交付时一起确认:

1. **`~/Documents`、`~/.ssh` 这类"删用户数据"要不要留在 FS-003?** **建议:不留。** 规则的名字是"删根/家目录";
   删某个子目录是另一件事,语料里恶意侧零样本,加进来就是没量过的新面。需要时另开。
2. **顺带认同义旗标拼法?** **建议:认。** `rm -fr /` 与 `rm -rf /` 是同一个命令,只认一种等于给了一个一字母的绕过。

**实现中攒下(2026-09-29)**

- W2 第一版把 `"$HOME"` 写成 `"?\$\{?HOME\}?"?`,两个引号各自可选,于是 `rm -rf "$HOME"/.cache/build` 里的闭引号被当成了词边界,命中。
  改成把带引号的形式整体作为一个分支(`"\$\{?HOME\}?/?\*?"`),补了 `rm -rf "$HOME/"`(必响)与 `rm -rf "$HOME/.cache"`(必静)两例钉住。
- **真机 `scan --root ~/.claude`:`FS-003` 34 → 8**,`overall` 69 不变。去掉的 26 条是 gstack 文档表格里的 `rm -rf /var/data`、
  Dockerfile 的 `rm -rf /var/lib/apt/lists/*`、`rm -rf /tmp/*`、`rm -rf ~/.gbrain…` 这类具体子路径。
  剩下 8 条**全在测试文件里**,是把 `rm -rf /` 当注入载荷来断言被拒(`assert.throws(() => pm.getRunCommand('test; rm -rf /'))`),
  改之前就在那 34 条里;与语料里两份护栏文字同属"提及不是执行",不为它加判断。

## 完成

```
合入:PR(2026-09-29;sha 合入后用 git log --grep P-029 找)
发布:v0.14.0
证据:TestDetect_RecursiveDeleteOfATreeRoot(internal/detect/rmrf_test.go)——17 必响、10 必静;W1 在 W2 前红
     (引号/花括号 HOME 与五种旗标拼法漏、五条子路径误报),W2 后绿;
     make bench(corpus 4f964622):benign 121 → 115、malicious 105 不变、0 丢失、hard negative 4/19、3539 全 scored、
     fixtures 与 2026-09-29b 逐字节同 —— baselines/results/aguard/2026-09-29c/;
     真机 scan --root ~/.claude:FS-003 34 → 8(剩下全是测试文件里的注入载荷字符串);make verify: all gates passed
```
