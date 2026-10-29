// 站上 markdown 的中文排版检查：调 zhlint，文件列表由这份脚本自己展开。
// npm 脚本在 Windows 上走 cmd，通配与 sed 都不可靠，所以列表不能交给 shell。
//
// 跑法：node tools-site/typography.mjs          只报
//       node tools-site/typography.mjs --fix    能自动修的直接修
//
// 逐份调用，不能一次把整张列表交给 zhlint：实测它只报第一个有问题的文件，
// 后面几份的错误会被吞掉，"0 处违规"因此是假的。
//
// 配置在仓库根的 .zhlintrc，三处按站上正文的写法关掉：方括号与花括号保持半角、括号外侧
// 不塞空格；全角引号 `“……”` 紧贴汉字，外侧不塞空格；加粗小标题以句号收尾时，句号后面那个
// 空格是 markdown 里的分段（`**说明。** 正文`），不算排版空格。
import { spawnSync } from 'node:child_process'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')
const repoRoot = resolve(siteRoot, '..')
const fix = process.argv.includes('--fix')

// zhlint 要的是相对仓库根、用正斜杠写的路径：site/learn/021_index.md
const rel = (f) => f.slice(repoRoot.length + 1).split(sep).join('/')

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === '.vitepress' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else if (name.endsWith('.md')) yield p
  }
}

// site/README.md 是仓库里的目录说明，不是网页：它引用 srcDir 之外的路径，也不在构建范围里
const files = [...walk(siteRoot)].filter((f) => !f.endsWith('README.md'))

// Windows 上全局装的 zhlint 是一个 .cmd 壳，只能走 shell；走 shell 就要把参数拼成一条命令，
// 拼的时候每个路径都加双引号，路径里不含引号与空格以外的特殊字符（仓库内成立）。
function runZhlint(args) {
  const command = ['zhlint', ...args.map((a) => `"${a}"`)].join(' ')
  return spawnSync(command, { cwd: repoRoot, shell: true, encoding: 'utf8' })
}

if (fix) {
  for (const f of files) {
    const before = readFileSync(f, 'utf8')
    runZhlint([rel(f), '--fix'])
    const now = readFileSync(f, 'utf8')
    const cleaned = now.replace(/ ([“「『])/g, '$1').replace(/([”」』]) /g, '$1')
    if (cleaned !== now) writeFileSync(f, cleaned, 'utf8')
    if (before !== readFileSync(f, 'utf8')) console.log('修过', rel(f))
  }
}

const total = (out) => {
  const found = /Found (\d+) error/.exec(out)
  return found ? Number(found[1]) : 0
}

let bad = 0
for (const f of files) {
  const run = runZhlint([rel(f)])
  const out = `${run.stdout || ''}${run.stderr || ''}`
  const n = total(out)
  if (!n) continue
  bad += n
  console.log(`\n✗ ${rel(f)}  ${n} 处`)
  for (const l of out.split('\n')) {
    const m = /\.md:(\d+):(\d+) - (.*)$/.exec(l.trim())
    if (m) console.log(`    第 ${m[1]} 行第 ${m[2]} 列：${m[3]}`)
  }
}

if (bad) {
  console.log(`\n排版检查：${files.length} 份文件，${bad} 处违规。`)
  process.exitCode = 1
} else {
  console.log(`排版检查：${files.length} 份文件，0 处违规。`)
}
