# Nef 官网

Nef 语言文档站的源码，用 VitePress 编写。`index.md` 是首页，六个板块各占一个目录。

## 依赖

需要 Node.js（本仓库在 v24.18.0 上开发与验证）。站点依赖只有 `vitepress` 与 `markdownlint-cli2`，版本在 `package.json`。

站点的示例代码由编译器的命令行程序核对，脚本在仓库根查找 `nf.exe`（Windows）或 `nf`（其他系统）。改过编译器后要重新生成：

```text
cd compiler
go build -o ../nf.exe ./cmd/nf
```

## 开发

```text
npm install
npm run dev
```

启动地址由终端打印出来。正文改动是热更新。

## 构建与部署

```text
npm run build
npm run preview -- --port 5200 --host 127.0.0.1
```

`npm run build` 输出到 `.vitepress/dist`，是纯静态文件。部署走 GitHub Pages：仓库里的 `.github/workflows/deploy.yml` 在 `main` 有推送时构建并发布，站点挂在仓库名那一段路径下。

路径前缀由环境变量 `BASE_PATH` 决定（`config.mts` 读它）：工作流按仓库名算出 `/仓库名/`；换成自己的域名时不设这个变量，就落在根路径。本地想看子路径的效果，跑 `BASE_PATH=/nef/ npm run build`。

`npm run preview` 在本地预览这份构建产物。

## 内容检查

```text
npm run check
npm run ledger
npm run rhythm
npm run names
npm run verify    # check + ledger + rhythm + structure + typography + names + build
```

| 脚本 | 查什么 |
| --- | --- |
| `check` | 代码高亮的记号表与编译器的记号流一致；每个示例能通过编译，每个标记为错误写法的示例会被拒绝；导航和侧栏里每条链接都指向存在的页面 |
| `ledger` | 把示例编译成可执行文件运行一遍，输出与正文标注的预期逐行比对，结果写进 `核对清单.json` |
| `rhythm` | 检查段落长度与用词，并确认页面里没有开发进度 |
| `structure` | markdownlint 查 markdown 结构：标题层级、表格列数、断裂的行、行首制表符（配置 `.markdownlint-cli2.jsonc`，与站内写法冲突的九条关掉并写明理由） |
| `typography` | zhlint 查中文排版：中英文之间与引号内外的空格、全角半角标点（配置在仓库根的 `.zhlintrc`） |
| `names` | 页面一级标题、侧栏与导航的文字、正文里的链接文字必须是同一个词；教程那一栏允许"第 N 课："前缀，顶部导航那六个板块名不要求等于某页标题 |
| `build` | 生成站点，同时检查正文里的相对链接 |

代码块标记有五种：`nf`、`nf-bad`、`nf-frag`、`nef`、`text`。

七项检查各自的实现在 `tools-site/`，跑法写在 `package.json` 的 scripts 里；这五种标记各自的规矩由 `tools-site/check-site.mjs` 钉住。

`tools-site/` 里还有一条不属于检查的脚本：`make-third-party.mjs`（`npm run third-party`）重新生成 `public/third-party-licenses.txt`，那是站上发布物里那些第三方依赖的许可与版权行清单，依赖变动之后跑一次。

## 目录

```text
index.md              首页
learn/    021–032     教程：第 0 课加 11 课
reference/ 033–043    语法与语义参考，034 是语法速查，043 是术语表
stdlib/   044–058     标准库：总览一页，十四组每组一页（`number` 与 `math` 合成一页）
tools/    059–061     命令行工具、错误信息索引、编辑器与调试
libs/     062–063     用 Nef 编写的外部库：web_test 与 test_run
about/    064–066     设计取舍、许可与商标、版本与兼容
tools-site/           五个检查脚本（`check`、`ledger`、`rhythm`、`typography`、`names`）、一份给 `check` 与 `ledger` 共用的例子分堆规则、一份生成第三方许可清单的 `make-third-party.mjs`
.vitepress/           站点配置与主题（config.mts、theme/）
public/               favicon、logo、`third-party-licenses.txt`、`inter-font-license.txt` 等原样拷进产物的文件
```

文件名前的数字是阅读顺序，一个目录占一段连续号码。新页面放进对应板块的目录，编号接在该目录号段末尾，再到 `config.mts` 的导航与侧栏登记。

站点没有目录页，链接必须写到具体页面，例如 `/learn/021_index`。

## 写内容时

侧栏标题与页面一级标题用同一个词。

- 一个页面只讲一件事，超出的部分留给下一课或参考页。
- 报错示例里那句 `// 报：……` 要抄编译器的原文，不要凭记忆写。
- 正文不写开发进度，不引用内部文档的章节编号。
- 不引用只在本地留存的材料（`docs/` 那九份技术文档、`internaldocs/`、决定编号）；克隆这个仓库的人打不开它们。这条由 `compiler/cmd/nf/public_docs_test.go` 扫两份 README、站上全部页面与另外两份 README 钉着。
- 会随每轮变动的统计数（测试函数条数、报错句子数、页面份数）不写进页面。
- 仓库里说惯了的词不进正文（钉住、挂上、借看、只借不交、写代码的人、站上……），这一批钉在 `tools-site/rhythm.mjs` 的禁词表上；编译器原话写在行内代码里，不受这一张表管。
- 语言规则本身以 `reference/` 那几页为准，用词阈值与禁词表在 `tools-site/rhythm.mjs`。

## 许可

网站上的文字按 CC BY 4.0，全文见本目录的 `LICENSE`，同一份文本的出处在 `nef-licensing/LICENSE-DOCS.md`。

代码按 Apache-2.0 加输出例外，全文见仓库根的 `LICENSE` 与 `NOTICE`。
