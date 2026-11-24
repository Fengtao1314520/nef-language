# Nef 语言内部库完成度与精致度分析报告

- 分析日期：2026-11-23
- 分析范围：`F:/GitLib/NF_Language` 全仓；重点为编译器约 5.4 万行 Go 源码、15 组标准库、LSP/DAP/包管理工具链，以及 `site/`、`docs/`、`internaldocs/`、`CHANGELOG.md` 的规格与测试体系
- 方法：逐组比对「公开 API 文档 → 检查期签名表 → 降级实现 → 运行期例程 → 端到端测试」，并对高危结论逐行复核
- 说明：本报告只读分析，未修改仓库任何文件。下文路径均相对仓库根目录，行号可直接跳转核对

---

## 0. 结论速览

**总体判断：这套代码不是「粗制滥造」，而是「骨架完整、局部有明显未收口的洞，且文档与实现存在系统性漂移」。**

仓库有相当扎实的质量基础设施：浮点打印有十万级随机逐值比对、堆分配有按类型的增长基线并被测试钉死、站点示例有编译与输出核对、报错原文有固化清单、1106 审查报告也确实推动了多项修复。所以下面列出的问题，多数不是「没人管」，而是「已知取舍没有写进规范」「错误路径没覆盖」「按线程隔离没贯彻到底」。

但确实存在必须优先处理的硬问题：

| 等级 | 数量 | 性质 |
| --- | --- | --- |
| P0 | 7 项 | 可触发堆破坏、越界写、资源失控、跨线程数据竞争 |
| P1 | 约 20 项 | 资源泄漏、语义边界错误、check/lower 诊断割裂、文档与代码漂移 |
| P2 | 一组 | 有意裁剪但披露不一致、编译器与工具链能力缺口、测试覆盖盲区 |

最值得先动的四件事：

1. `base64` 非法长度路径释放未初始化栈槽 —— 潜在堆破坏，且现有测试测不到
2. `wideToText` 把长宽字符按所需字节数全量写进 64 KB 缓冲 —— 注释说「截断」，实际写越界
3. `Socket.recv_into` 负偏移无校验 —— 可写到字节块头之前
4. `json.parse` 的解析状态是静态全局 —— 两条线程会互相踩数据

---

## 1. P0：正确性与安全问题（建议立即修）

### 1.1 base64 解码：跳到错误出口时 `@free` 一个从未赋值的槽位

- 证据：`compiler/internal/runtime/base64.go:277-297` 先判长度是否为 4 的倍数，不是则 `Jcc(NE, bad)`；而本调用的输出句柄 `dOut` 直到 `:297` 的 `@alloc` 之后才 `put(dOut, RAX)`。
- 共用错误出口 `base64.go:437-440` 无条件 `get(RCX, dOut)` 后 `@free`。
- `compiler/internal/runtime/runtime.go:1281-1285` 的 `prologue` 只调整 `rsp`，不清零栈帧；槽位是固定偏移。因此非 4 倍数输入（如 `"YQ"`）跳到 `bad` 时，`dOut` 里是上一次调用残留的栈内容；`@free` 只对 null 和零尺寸块放行，残留值若指向活跃堆块，会造成双重释放与堆破坏。
- 现有测试 `compiler/internal/lower/bytes_e2e_test.go:493-498`、`text_more_e2e_test.go:73-78` 只断言错误文本与退出码，**不检查堆**，所以全绿不等于安全。
- 修到什么程度：拆分错误标签或把 `@alloc` 提前到长度校验之前，保证「释放的一定是本调用自己造的块」；补一条循环喂非法长度、断言堆用量不增长且不崩的测试。

### 1.2 `wideToText`：注释承诺截断，代码按全量所需字节写入固定缓冲

- 证据：`compiler/internal/runtime/os.go:544-561` 的注释明确写「落点是这一条线程的 `aBuf`，装得下 `lineCap` 个字节……真的超过了返回的就是截断的那一段，这一条语言规则尚未定」。
- 但 `os.go:562-596`：第一次 `WideCharToMultiByte` 问出所需字节数存 `wBytes`，第二次直接把 `wBytes` 作为 `cbMultiByte` 传入，目标却是 `aBuf + strHead`，全程没有按 `lineCap` 钳制。
- 缓冲布局：`compiler/internal/runtime/runtime.go:789-793`，`aBuf` 只有 `strHead + lineCap`（65536）字节，后面紧挨 `findBuf`、`enumPat` 等格。
- 触发面：`environment.get`、`environment.names`、`environment.current_directory` 均走这里（`os.go:167/1006/1371`）。Windows 单个环境变量值可达约 32767 个宽字符，中文按 UTF-8 每字 3 字节即约 98 KB，足以越界覆盖后续状态格。
- 修到什么程度：第二次调用把 `cbMultiByte` 钳到 `lineCap`；同时裁定超长语义（推荐抛 10060/10051 或明确公告截断），把「语言规则尚未定」从交付代码里清掉；补长环境变量 e2e。

### 1.3 `Socket.recv_into(buffer, offset)`：负偏移可写到块头之前

- 证据：`compiler/internal/runtime/net.go:943-960`：`room = BytesLen - offset`，负数 offset 反而使 `room` 大于块长；`目标 = BytesBody + offset` 指到块头内部甚至分配区之前，随后 `recv` 按该长度写入（`:960`）。
- 检查期只声明 `offset: int`（`compiler/internal/check/expr.go:1193-1196`），降级期只透传（`compiler/internal/lower/lower.go:1601-1611`），没有下界校验。
- 另外，超过上界时 `:949-951` 把 `room` 置 0，而 `recv` 返回 0 在调用侧被当成「对方已关闭」，语义也被混淆。
- 修到什么程度：入口直接拒绝 `offset < 0 || offset > BytesLen`（可复用 10031），上界外也抛错而不是谎报连接关闭；补负偏移与越界偏移两条 e2e。

### 1.4 JSON 解析状态是静态全局，线程会互相踩

- 证据：`compiler/internal/lower/json.go:212-228` 的 `jstate`（`src/buf/len/pos/err/errAt/esc/trail/depth`）全部是 `ir.G` 静态槽；`:239-247` 用 `l.hidden()` 申请，而 `compiler/internal/lower/lower.go:6586-6596` 明确 `hidden()` 占的是静态池。
- 使用处：`compiler/internal/lower/jsonparse.go:54,86-105,617-622` 每次调用直接 `setG(j.pos/err/...)`；`jsonwrite.go:107-147` 的环检测链 `trail/depth` 也是同一组全局，两线程同时进入会双重分配、互相覆盖。
- 而文本与 class 句柄允许跨线程传入（`compiler/internal/check/thread.go:150`、`site/stdlib/067_thread.md:11`），worker 完全可以调 `json.parse(text)`。
- 对照：`site/stdlib/058_http.md:154` 明写「这一组的临时数据按线程各一份」，说明项目已有按线程隔离的设计共识，json 组没有贯彻。
- 修到什么程度：把 `jstate` 迁进每线程状态块（仿照 net/http/regex 的做法），或至少给 `trail/depth` 加运行期锁；补「两条线程各解析一万次，结果与单线程逐字一致且堆不涨」的 e2e。

### 1.5 文件读写失败路径成片泄漏操作系统句柄

- 证据（仅列最具代表性的几处）：
  - `compiler/internal/runtime/file.go:308-350`：`CreateFileW` 成功后，`:322`（取大小失败）、`:343`（读失败）、`:347`（短读）都跳 `noFile`（`:412-417`），三条路径都不 `CloseHandle`。
  - `compiler/internal/runtime/write.go:97-126`、`:192-221`：写失败跳 `fail` 时同样不关句柄。
  - `compiler/internal/runtime/fileops.go:457-469`（`read_from`）、`:647-685`（`write_at`）：定位/读写失败跳 `noFile` 时不关句柄。
- 失败路径还会漏掉已经 `@alloc` 的堆块（如 `file.go:328-329` 的 `xObj`）。
- 这些句柄不进语言堆，`TestWalkHeap`/堆增长测试完全测不到；循环失败会耗尽进程句柄表，之后所有文件操作静默失败。
- 修到什么程度：所有「打开—读/写—关闭」改成统一出口，`CloseHandle` 覆盖每条失败分支；补「失败 N 次后仍能正常打开文件」的回归测试。

### 1.6 子进程表满时抛错，但刚创建的进程变成无人管理的孤儿

- 证据：`compiler/internal/runtime/proc.go:307-312`：走到 `full` 标签时，`CreateProcessW` 已经成功（`:263`）；代码只释放锁并调 `@proc_fail`，既不把进程句柄写进表，也不 `CloseHandle` 刚创建的进程与线程句柄（正常路径见 `:295-302`）。
- 影响：第 129 次 `process.start` 会启动一个真实进程，抛 10112 后调用方既拿不到 `Process` 句柄，也无法 `close()` 结束它。这与文档「最多同时 128 个，超出抛 10112」的承诺不符——多出一个失控进程。
- 修到什么程度：`full` 分支在放锁前 `TerminateProcess` + `CloseHandle`；补「填满 128 个再起一个，系统里没有第 129 个进程」的测试。

### 1.7 `time.elapsed()` 在第二条线程上返回「开机至今」

- 证据：`compiler/internal/runtime/os.go:443-469` 的初始化守卫只看 `perfFreq`；`perfFreq` 在静态数据段（`os.go:89`，全程序共享），而计时起点 `perfStart` 在每条线程的状态块里（`compiler/internal/runtime/runtime.go:815`，开局为 0）。
- 线程 A 调过一次后 `perfFreq != 0`；线程 B 首次调用跳过初始化，直接拿本线程为 0 的 `perfStart` 做减法，返回系统开机至今的秒数。`os.go:81-86` 的注释自己就写明了「起点在每条线程的状态块、频率是全机一个」，但守卫没有体现这一点。
- 修到什么程度：把「是否已初始化」判据和起点放进同一作用域（每线程 `perfReady` 槽，或起点改静态）；补主线程/子线程 `elapsed` 互相接近的 e2e。

---

## 2. P1：资源泄漏、语义边界与文档漂移

### 2.1 运行期资源泄漏（按影响排序）

| 问题 | 证据 | 后果与建议 |
| --- | --- | --- |
| 正则三块缓冲换新不释放、线程退出也不归还 | `compiler/internal/runtime/regex.go:188-235` 三处 `@alloc` 后直接 `storeCell`，无 `@free`；`runtime/thread.go:313-333` 只释放状态块本体，全仓查不到对这三个名字的 `@free` | 循环换表达式或起线程会持续涨堆；修到「换 100 次表达式、起 100 条线程后回到基线」并补 heap slope 测试 |
| `net.connect` 超时/`select` 失败不释放 `GetAddrInfoW` 结果 | `compiler/internal/runtime/net.go:529-534`、`:638-645` 直接抛；正常路径 `:611`、`:632` 有 `FreeAddrInfoW` | 心跳/重试程序稳定累积系统内存；两处抛错前补释放 |
| HTTP 正文用 `Json` 时，中转字节块在 `@http_begin` 抛错路径泄漏 | `compiler/internal/lower/lower.go:5444-5455` 造块，`:5515-5517` 只在成功路径释放，且未登记帧清理 | 坏 URL/超时每请求泄漏一块；登记到清理表并补坏 URL 循环测试 |
| dict/hashset 墓碑永不压缩 | `compiler/internal/runtime/dict.go:565-568` 注释自陈；扩容判据 `:479-491` 只算活跃数 | 大批量删改后探测链退化；把墓碑计入重摆判据 |
| 文本在容器位置/`struct`/`class` 字段里有意不归还 | `compiler/internal/lower/destroy.go:30-36`、`destroy_e2e_test.go:207-211`、`docs/018_性能基线.md:162/164/185` | 属设计取舍，但 `site/stdlib/044_index.md` 的归属章节未写这个例外；至少补齐文档，否则用户会误以为零泄漏 |

### 2.2 数值与输入边界

- **JSON 可渲染出非法 JSON**：`compiler/internal/lower/jsonparse.go:162-180` 允许 `1e999` 溢出成无穷，`jsonops.go:304-307`、`jsonwrite.go:195-203` 直接走 `@float_str`，而 `compiler/internal/runtime/float.go:1342-1352` 输出 `NaN`/`-Inf`/`Inf`。`json.parse("1e999").to_json()` 得到 `Inf`，再解析会失败；`site/stdlib/056_json.md:263` 只说了字面量不接受。建议解析或渲染阶段改成 `null`/失败。
- **HTTP 端口累加无溢出检查**：`compiler/internal/runtime/http.go:590-605` 只有循环结束后的 `>65535` 检查，超长数字串可回绕成合法端口，静默连到意外目标。建议逐位按 `(2^64-1)/10` 判溢出。
- **`s.repeat(count)` 是 O(n²) 且无字节数溢出检查**：`compiler/internal/runtime/textops.go:1353-1405` 每轮调一次 `@str_cat`；对比 `strPad`（`:1025-1154`）已用翻倍拼接。大 count 会极慢，长度回绕可能申请小块后越界写。`docs/017_运行期例程.md:126` 还把逐次拼接写成规格，需同步更新。
- **容器打印不转义**：`compiler/internal/lower/print.go:381-385` 直接给文本套一对引号，含引号或换行的文本会打印出歧义甚至跨行输出。可复用 `text.escape_json` 的逻辑。

### 2.3 检查期与降级期割裂（最影响用户信任的系统性问题）

- `compiler/internal/lower/lower.go:431-433` 把所有降级失败统一包装成「后端还没做到这些：…」；`lower.go:286-362` 的错误不带行列，`compiler/cmd/nf/main.go:154-157` 原样打印。
- `compiler/internal/ls/diagnostic.go:120-140` 只跑 `check.Run`，不跑 lower —— 编辑器永远看不到这类拒绝。
- 结果：**`nf check` 绿灯、`nf build`/直接运行才失败，且报错没有位置**。仓库已有明确的迁移先例（`compiler/internal/check/earlyreject_test.go:5-11` 记录了三处同类修补），但远未收尾；`internaldocs/004_里程碑与发布.md:208` 说「九条还不支持」，当前 `lower.go` 只剩 5 条（`:3524/3743/4075/5967/6382`），文档数字也过时。
- 修到什么程度：给 lower 错误定义带 `File/Line/Col` 的结构化类型；把可判为语言规则的拒绝全部迁回 checker；错误分类要能区分「写错了」与「编译器没做完」。

### 2.4 「编译期全查」承诺未兑现

- 网站与设计文档承诺所有检查在编译期完成（`site/about/064_design.md:7`、`internaldocs/001_SPEC.md:16`），但借用归还仍有运行期兜底表：10097/10098 在 `compiler/internal/runtime/runtime.go:73,296,3838-3906`，`compiler/internal/lower/borrowfree_e2e_test.go:16` 把它固定下来。
- 1106 审查报告 `internaldocs/1106审查记录/NF审查报告.md:133-188` 称这是「唯一一处运行期代劳」且「维持现状是唯一不可接受的选项」；目前只是缩小了适用范围（直接写法已前移），跨包/方法/摘要看不到的写法仍走运行期。要么改实现，要么改承诺，两头不占的状态应结束。

### 2.5 文档与实现的系统性漂移

| 位置 | 现状 | 建议 |
| --- | --- | --- |
| `site/reference/041_errors.md:151,153-167` | 声称「json 组不抛任何错误」，整张错误码表没有 10114；但 `site/stdlib/056_json.md:33-35`、`site/tools/060_errors.md:664` 明写 `j.set`/`j.add` 抛 10114 | 这是最硬的文档矛盾；补 10114 并改正承诺 |
| `site/stdlib/052_environment.md:12,15` | `environment.set`、`current_directory` 错误码栏写「不抛」；`check/docs.go:433-436` 英文甚至写 `change_directory`「never throws」 | 代码 `runtime/os.go:1057,1061,1375` 实际抛 10060/10051；统一三处文档 |
| `site/stdlib/052_environment.md:61` 与 `compiler/nf_sigs.tsv:17` | 旧名 `set_current_directory` 仍在 | 改成实际的 `current_directory` |
| `compiler/nf_sigs.tsv:4,86-87` | 仍是 `bytes.new`、`math.maximum/minimum`，且没有 v0.9.0 新增的 `net`/`http`/`thread`/`format_utc` 等 | 该台账无人校验；要么重新生成并纳入自动检查，要么删掉 |
| `site/stdlib/067_thread.md:7,101` vs `:13` | 正文写 `t.id()`，表格/签名表 `expr.go:1229` 写 `Thread.number()` | 正文改成 `number()`，或加 `id()` 别名并同步四张表 |
| `site/stdlib/046_text.md:13,44` vs `:206` | 同页既列 `text.to_float`，又说「小数的解析没有对应的函数」 | 删除过时段落；站点逐句比对只覆盖表格说明列，补正文核查 |
| `site/stdlib/046_text.md:34` | `text.to_int` 错误码只写 10012；`runtime/text.go:505-508` 溢出抛 10013，`060_errors.md:617` 也写了 | 表格补 10013 |
| `docs/017_运行期例程.md:221` | 写 `@path_dir` 返回带分隔符；实现 `runtime/os.go:1148-1162` 与 `site/stdlib/051_path.md:14` 都不带 | 更新内部规格 |
| `compiler/internal/rtjson/json.go:20` | 注释说产物在 `internal/runtime/json.go`，该文件不存在 | 改成 `lower/json*.go` |
| `internaldocs/001_SPEC.md:1702` | 仍说 dict 值「摘不出来」；实际已有 `d.take` | 更新规格 |

### 2.6 平台与语义边界（文档未披露或规则未定）

- `file.remove_tree` 复用带 64 层上限的遍历（`compiler/internal/runtime/fileops.go:1338-1341,1478-1480`），深树必然失败，但 `site/stdlib/050_file.md:38` 未写这条限制（同页 `list_names_tree` 写了）。
- `file.ensure_directories` 对 UNC 路径返回假，内部规格 `internaldocs/001_SPEC.md:1031` 承认，公开文档未写。
- `file.list_names` 静默跳过超长条目（`runtime/fileops.go:357-359`），公开文档未提；`site/tools/060_errors.md` 也未覆盖。
- `time.format_utc` 对负年份补零成 `00-1`（`compiler/internal/lower/lower.go:5315-5329` 走 `@str_pad`），与 `site/stdlib/054_time.md:11`「负数照实写」矛盾。
- `p.wait()` 把 32 位 `DWORD` 当 64 位读（`runtime/proc.go:381-382`），`exit(-1)` 返回 4294967295，文档未说明。
- `@args` 单参数超过 `wideCap=4096` 宽字符时 `putWide`（`runtime/file.go:686-698`）无边界检查；Windows 命令行单参数可达 32767 字符。
- 多处「语言规则尚未定」留在生产代码注释中：`runtime/textops.go:278`（全角空格 trim）、`runtime/os.go:550`（超长环境变量）、`runtime/http.go:556`（URL 百分号转义/用户名密码）。建议裁定并写入规范。

---

## 3. P2：有意裁剪、工具链缺口与测试盲区

### 3.1 有意裁剪，但披露程度不一

以下功能确实没做，且多数在规格里写明「不做」，属于产品取舍而非缺陷；问题在于**公开文档披露不完整**，容易被当成「没做完」：

- 正则最小子集：`{m,n}`、非贪婪、`\b\B`、反向引用、`(?...)` 全不支持（`compiler/internal/rtre/compile.go:266,294,343,345`），捕获组与 `$1` 替换引用也未实现（`rtre/run.go:211-214`、`runtime/regex.go:1564-1565`），但 `site/stdlib/055_regular_expression.md:63` 没提 `$1` 与字符组内 `\D\W\S` 不可用。
- `regex.find_all` 在 `internaldocs/003_开发日志.md:2925` 被明确记为「按判据没有实现」。
- net 无 UDP/原始套接字/按行 IO/锁、服务端一次只服务一个连接；http 不支持分块接收、自定义根证书、异步、百分号还原与用户名密码（`site/stdlib/057_net.md:84-88`、`058_http.md:237-243`）。
- process 不接收子进程输出、不换目录启动（`site/stdlib/053_process.md:7,56`）。
- 无时区数据库（`site/stdlib/054_time.md:66-68`）、文本只处理 ASCII 大小写与空白、无双曲函数/`atan2`/`hypot`、容器无 `list.contains`/`dict.items`/`hashset.is_subset`（`internaldocs/002_决定记录.md:700-716`）。
- 闭包/匿名函数、多返回值、`interface` 不能当类型等是语言层面的明确取舍（`NF审查报告.md:535-536`、`001_SPEC.md:193,521`）。

建议：把「不支持清单」集中成一页「已知限制」，并让 `check` 的悬停文档与站点同步引用，避免用户逐个文件翻。

### 3.2 编译器与工具链半成品

- **泛型**：无约束（不做 `where`/`extends`），模板不被实例化时字段与体不检查（`internaldocs/004_里程碑与发布.md:874,897`）；`check/generic.go:298-327` 用 `rounds > 1000` 后 `panic`，`ast/clone.go:188,243,266` 遇到未接住的节点直接 `panic`。应改成带位置的编译错误。
- **线程**：跨线程类型白名单只允许值类型/文本/class 句柄（`check/docs.go:651-652`），理由是「归还点还没按线程各一份定下来」；DAP 固定 `threadID=1`（`dap/dap.go:65-71`），无法调试并发。
- **DAP**：无表达式求值器（`dap/dap.go:147-148,345-366`），复合值 `variablesReference` 恒为 0 无法展开（`:337`），无条件断点/异常断点/函数断点等能力。
- **LSP**：只处理约 15 个方法（`ls/server.go:95-307`），无 code action/workspace symbol/类型定义/文档高亮；畸形报文静默丢弃（`:53-55`），`recover` 吞掉内部 panic 且不写日志（`:86-93`），`serverInfo.Version` 留空（`:111`）。
- **包系统**：无 semver 与锁文件（`pkgfmt.go:72` 的 `Deps` 写死版本），`pkgload.go:154-184` 用 `runtime.GOOS == "windows"` 猜大小写敏感性并在注释里写「等它进范围时改成探针，别现在猜」，包身份是路径字符串；`nf` 命令没有 install/publish/test。
- **死代码与占位**：`compiler/goalloc/`、`gocalc/`、`golo/`、`index/` 四个目录完全为空；`check/expr.go:1759-1760` 丢弃一个恒为 nil 的 `memberRange` 错误；多处生产路径 panic（`codegen/frame.go:130,147`、`x64/asm.go:126,180,299`、`runtime/runtime.go:887,1144`）。
- **`nf_sigs.tsv` 与 CHANGELOG 的 API 名分歧**：`CHANGELOG.md:359` 说改名为 `math.minimum/maximum`，实际 API 是 `math.smaller/larger`（`check/expr.go:2592-2593`、`site/stdlib/048_number.md:87`）。

### 3.3 测试与校验体系的盲区

- `site/tools-site/check-site.mjs:1-7` 的五条检查只覆盖 token 一致、`nf check` 通过、错误示例被拒、链接存在、写死的数字对齐。它**不核对 API 名字/签名、错误码表、散文文案，也不执行示例**；这就是 041_errors 漏 10114、052 残留旧 API 名能长期存活的原因。
- `site/核对清单.json:2-8`：171 条示例中 130 条真跑过并对输出，41 条（约 24%）只有编译检查，没有运行期断言。
- 「不支持」类能力测试覆盖不均：正则不支持写法有 14 种逐条断言（`lower/regex_e2e_test.go:143-170`），但「无 UDP/无锁/无时区表/无子进程输出/无 Unicode 大小写/无双曲函数」等基本只有文案，没有回归测试；一旦误放开，站点检查与 e2e 都不会红。
- 网络与浏览器相关测试在本机长期红并被归为环境问题（`CHANGELOG.md:33,51,66`、`issue10-reply.md:23`）：`TestHttpCodes`、`TestNetCodes`、`TestWebTestSiteCheck`、`TestWebTestRunAllCheck`。
- `webtest/README.md:182-187`：库的连接与进程句柄放在包顶层状态，一个进程只能连一个浏览器，启动第二个会覆盖第一个；无任何回归用例。

### 3.4 1106 审查报告里至今未闭环的项

`internaldocs/1106审查记录/NF审查报告.md`：

- §2.1 运行期 10097 兜底（见 2.4）
- §6.2/§6.3「文档是第二套实现、没有同步机制」「代码即事实的自动对齐仍未建立」——本轮发现的文档漂移全部是其后续例证
- §2.10 `new C()` 引用字段停在 `null`、首次使用即 10092，未做编译期强制初值
- §2.7 `interface` 不能当类型
- §4.8 退出码/错误码分段建议未采纳，仅用文档说明代替
- §3.4 时区数据与闭包推迟到 1.0 之后
- v0.9.0 已发布，但报告 §3.1 提出的「外部程序验证」判据没有看到正式复核记录（流程未闭环）

---

## 4. 建议路线图

**第一阶段：止血（正确性）**

1. 修 1.1 base64 未初始化释放、1.2 `wideToText` 越界、1.3 `recv_into` 负偏移 —— 三项都是可被输入触发的内存破坏
2. 修 1.4 JSON 状态线程隔离、1.7 `time.elapsed` 起点作用域 —— 并发正确性
3. 修 1.5 文件句柄、1.6 孤儿进程 —— 资源与进程可控
4. 每项配套 heap slope / 句柄计数 / 并发一致性回归测试

**第二阶段：收口（语义与文档）**

1. lower 错误加源码位置并系统性迁回 checker；LSP 暴露该通道
2. 补齐 2.1 的泄漏与 2.2 的边界；裁定所有「语言规则尚未定」
3. 以「错误码单一注册表 + API 签名单一来源」重构文档，`041_errors`、`052`、`nf_sigs.tsv`、`CHANGELOG` 一次性对齐；把 API 名与错误码纳入 `check-site.mjs`
4. 把有意裁剪集中成一页「已知限制」，公开文档与悬停文档同源

**第三阶段：精致度（工具链与语言）**

1. DAP 表达式求值器与复合值展开、LSP code action/workspace symbol
2. 泛型模板独立检查、把生产路径 panic 改成带位置的诊断
3. 包身份规范化、版本范围与 install 命令
4. 补正则 `$1`/`{m,n}` 与 `find_all`（若要追平通用正则）；否则明确定为永久边界

---

## 5. 复核说明

本报告中的 P0 七项，以及文件句柄泄漏、`net` addrinfo 泄漏、HTTP 端口溢出、JSON 非有限值、`041_errors` 矛盾、`thread` API 名矛盾，均已按引用行号逐段读码确认；其余条目来自文档/规格与代码的交叉比对，可作为进一步排查的入口。本报告没有实际运行崩溃用例，也没有改动任何源码、文档或测试文件。
