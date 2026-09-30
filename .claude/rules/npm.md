---
paths:
  - "npm/**"
  - "Makefile"
  - ".github/**"
---
## npm 分发(`npm/`,`make npm-dist`)

面向用户的说明在 [README.md](../../README.md#install);这里只写不要怎么改。
布局是**五个包**:一个 launcher(`bin/aguard.js` + 从自己 `package.json` 读名字)加四个平台包
(带 `os`/`cpu`,各装一份静态二进制)。参照的是 biome,不是 esbuild。

- **任何一个包都不许有 install 脚本。** 把编译产物放上 npm 最常见的做法是 postinstall
  下载二进制 —— 那正是本引擎在别人仓库里会报的形状(取回来再执行)。自己用这种方式分发,
  等于让工具违背自己 README 里"故意不提供 `curl | sh`"那一段。二进制**就在 tarball 里**,
  npm 按 `os`/`cpu` 挑。`TestNpmPackagesHaveNoInstallScripts` 盯着这条。
- **launcher 绝不能在没拿到判决时退 0。** 通行写法结尾是 `process.exitCode = result.status`,
  而子进程被信号杀死时 `status` 是 `null`,Node 于是退 **0** —— 在这个工具里 0 的含义是
  "低于阈值",也就是把一次被杀死的扫描渲染成一次干净的扫描。所以 launcher 里的非答案路径
  (平台不支持、平台包没装)**一律退 2**,信号则退 `128 + 信号号`,和"退出码 2 不是通过,
  扫描没有发生"是同一条。`TestNpmLauncherExitsTwoWhenThePlatformBinaryIsMissing`
  钉住其中最现实的那条(`--no-optional` 或跨平台 lockfile 装出来的半套)。
- **"退什么码"和"说不说话"是两个问题,后者按类别分而不是按单个信号分。**
  `ROUTINE_SIGNALS`(INT、TERM、HUP、QUIT、PIPE)是人或系统**请求**程序停止:Ctrl-C、关终端、
  `kill`、`aguard scan | head` 关掉管道,裸二进制对这些一个字都不打,launcher 也不许打 ——
  否则最常见的那种打断长扫描的方式,结尾会多出一行指着 `node_modules` 里某个文件的"错误"。
  剩下的(SEGV/BUS/ABRT/OOM 的 KILL)是二进制**失败**了,那才值得一行。第一版只特判了
  SIGPIPE(因为实测只碰到它),SIGINT 照样在刷噪音 —— **加信号前先问它属于哪一类**,
  `TestNpmLauncherReproducesTheBinarysExit` 两类各有用例。
- **`stdio: "inherit"` 是承重的**,不是顺手写的:`aguard hook` 从 **stdin** 读 JSON hook 事件,
  管道到不了子进程,闸门就永远看不到它被问的那个事件。
- **包名只在 `Makefile` 的 `NPM_NAME` 里出现一次。** launcher 在运行时用自己
  `package.json` 的 `name` 拼出平台包名,所以 shim 里写死 scope 会在改名后仍然解析到一个
  没人发布的包 —— `TestNpmPackageNameHasOneSource` 就是断言 shim 里**不含**那个名字。
- **`amd64` → `x64`**,这是 Go 和 npm 唯一一处拼法不同,也是这里唯一容易写错的地方。
- **平台清单是一条链,不许出现第二份拷贝**:`DIST_TARGETS` → launcher 的
  `optionalDependencies` → launcher **运行时从自己的 `package.json` 读出**支持的平台。
  `TestNpmLauncherPinsEveryDistTarget` 管前半段(多一个是发布悬空依赖,少一个是那台机器上
  装完直接退 2),`TestNpmLauncherKeepsNoPlatformListOfItsOwn` 管后半段 —— 它断言
  `npm/bin/aguard.js` 里**没有** `"darwin"`/`"linux"`/`"x64"`/`"arm64"` 这些字面量。第一版
  在 shim 里写死了一份 `SUPPORTED`,那种漂移别的测试都看不见:一个已构建、已 pin 的平台
  照样会被 launcher 以"no prebuilt binary"拒掉,而 npm 明明把二进制装好了。
- **平台包不写 `engines`。** 它里面只有一个二进制、一行 JS 都没有,约束不了任何东西;而作为
  optionalDependency,engine 不匹配会让 npm **静默跳过**它,于是 Node 16 的用户看到的是
  "平台包没装"而不是"你的 Node 太老"——把诊断指向了错的地方。Node 版本下限只写在 launcher
  上,那才是真正跑 JS 的包。
- **`npm-dist` 故意不依赖 `dist`**,只打包已经在 `dist/` 里的二进制。LDFLAGS 会盖构建时间戳,
  重跑一次 `dist` 就换了字节,于是 npm 里的二进制不再是任何一份已发布 `SHA256SUMS.txt`
  描述的那个文件。缺文件时它报错让人先跑 `make dist`,而不是自己悄悄重建。
- **发布顺序:npm 整段在 `gh release create` **之前**,其中先四个平台包、最后 launcher。**
  两层理由:npm 是**不可重做**的那一半(同一版本永远不能重发,而 GitHub release 删掉可以重建),
  所以它先跑,失败时不会留下一个"公告了 npm 上并不存在的东西"的公开 release;launcher 用精确
  版本钉住平台包,先发 launcher 会留下一个"装得到 launcher、装不到二进制"的窗口。
- **发布步骤必须能重跑**:每个包先 `npm view <name>@<version>` 判在不在,在就跳过。
  没有这一步,一次半途失败(token 过期、registry 5xx)只能靠**重打一个版本号**收场 ——
  重跑会在第一个包上就撞"cannot publish over the previously published versions"然后
  `set -e` 中止。
- **`make npm-dist` 自己拒绝未打 tag 的版本**(`NPM_VERSION_DERIVED` + 那条 grep)。
  以前这里写着"npm pack 会拒绝这种版本",**那是错的**:`0.8.1-2-gabc-dirty` 是合法的 semver
  prerelease,实测 npm 照收。真正的后果是手动发布会把一个垃圾版本号永久占掉(npm 不允许复用)。
  显式传 `NPM_VERSION=` 仍然放行 —— 那是明说的选择,不是 git 的副产物。
- **`--provenance` 现在没开,这是仓库现状不是偏好。** npm 的出处证明由跑 workflow 的那个仓库
  生成,要求它**公开**且与 `package.json` 的 `repository` 相符;而构建跑在私有源码仓库、
  `repository` 指向公开分发仓库,带上这个 flag 会让 publish 失败。要开就得改其中一个前提,
  workflow 里的注释写了这件事 —— 在那之前可验证的下载是 release 上的 `SHA256SUMS.txt`。
- **`npx` 和加载时闸门不能混用**,这是 npm 这条路新开的口子:`hook install` 注册绝对路径,
  npx 的路径在 npm 的 `_npx` 缓存里、之后会被回收,回收之后就是 `GATE-001`
  ——每个 skill 未经审计地加载,而外表和"受保护"一模一样。所以
  `cmd/aguard/gate.go` 的 `ephemeralExeWarning` 在 install(含 `--dry-run`)和 `hook status`
  两处都会警告。**名单只有 `_npx`/`_cacache` 两项,别往里加宽泛的东西**:在正常
  `npm i -g` 路径上响的警告会教会运维跳过警告,而这条恰恰是他必须读的
  (`TestEphemeralExeWarningNamesOnlyDisposablePaths` 两个方向都钉住了)。

