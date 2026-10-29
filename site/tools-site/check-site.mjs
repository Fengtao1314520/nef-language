// 站点的四条硬检查：
//   1. 高亮器与编译器的词法必须认同一套记号；
//   2. 站上每个 ```nf 代码块，都得能通过编译器的检查；
//   3. 每个 ```nf-bad 块（示范错误写法）必须真的被拒绝，且报出作者预期的那句话；
//   4. config 里每一条 link 都要对上一个真实存在的页面。
//
// 跑法：node tools-site/check-site.mjs   （或 npm run check）

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { tokenize } from '../.vitepress/theme/nef-highlight.mjs'
// 围栏块怎么切成"一篇"与"一个工程"只有一份规则（example-groups.mjs）：
// 台账那一头要照着它跑输出，这里要照着它送检查，两处各写一遍会不一致。
import { blocks, groupsOf, isEntry, walk } from './example-groups.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')
const repoRoot = resolve(siteRoot, '..')
const nfExe = join(repoRoot, process.platform === 'win32' ? 'nf.exe' : 'nf')

// ---------- 一、高亮器与编译器是否同一套记号 ----------

// 与 nf.exe tokens 对齐的口径：关键字与保留名都算 name；注释不进比较；
// 字符串与字符只比类型——编译器交回的是解码后的内容，高亮器看到的是原文。
function jsTokens(src) {
  const out = []
  for (const t of tokenize(src)) {
    switch (t.kind) {
      case 'nl': out.push('nl:\\n'); break
      case 'comment': case 'plain': break
      case 'keyword': case 'reserved': out.push('name:' + t.text); break
      case 'str': case 'char': out.push(t.kind); break
      default: out.push(`${t.kind}:${t.text}`)
    }
  }
  return out
}

// nf tokens 一行一个 `类型:文字`。字符串解码后的内容里可能真有换行，
// 那几行续行不带类型前缀，认不出来就丢掉——反正字符串只比类型。
function goTokens(file) {
  const raw = execFileSync(nfExe, ['tokens', file], { encoding: 'utf8' }).replace(/\r/g, '')
  const out = []
  for (const line of raw.split('\n')) {
    if (line.length === 0) continue
    const m = /^(nl|name|num|str|char|op):/.exec(line)
    if (!m) continue
    const kind = m[1]
    out.push(kind === 'str' || kind === 'char' ? kind : line)
  }
  return out
}

function checkHighlight(files, tmp) {
  let checked = 0
  const bad = []
  for (const f of files) {
    for (const b of blocks(readFileSync(f, 'utf8'))) {
      if (!/^(nf|nf-frag|nf-bad)$/.test(b.lang)) continue
      checked++
      const probe = join(tmp, `probe-${checked}.nf`)
      writeFileSync(probe, b.body)
      const js = jsTokens(b.body)
      let go
      try {
        go = goTokens(probe)
      } catch {
        continue // 词法都过不去的片段由第二项检查报，这里不比
      }
      if (js.join('|') !== go.join('|')) {
        bad.push({ file: relative(siteRoot, f), line: b.line, js: js.join(' '), go: go.join(' ') })
      }
    }
  }
  return { checked, bad }
}

// ---------- 二、例子：```nf 必须过检查，```nf-bad 必须被拒绝 ----------

// 块里的 `// 报：xxx` 是作者写下这句时看到的报错，编译器得原样给出。
function expectedMsgs(group) {
  const out = []
  for (const it of group.items) {
    for (const m of it.body.matchAll(/\/\/\s*报：(.+)/g)) out.push(m[1].trim())
  }
  return out
}

function checkExamples(files, tmp) {
  const bad = []
  let n = 0
  for (const f of files) {
    for (const g of groupsOf(readFileSync(f, 'utf8'))) {
      n++
      const dir = join(tmp, `case-${n}`)
      mkdirSync(dir, { recursive: true })
      const sources = g.items.map((it, i) => ({
        name: it.name ?? `part${i}.nf`,
        body: it.body,
        line: it.line
      }))
      for (const s of sources) {
        const to = join(dir, s.name)
        mkdirSync(join(to, '..'), { recursive: true }) // 包那种例子落在 `libs/包名/` 那一层下面
        writeFileSync(to, s.body)
      }
      const run = (s) => {
        try {
          execFileSync(nfExe, ['check', join(dir, s.name)], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
          return { out: '', failed: false }
        } catch (e) {
          return { out: ((e.stdout ?? '') + (e.stderr ?? '')).trim(), failed: true }
        }
      }
      // 只有摆在工程根的那一片才是入口：命令行只接受一个入口文件，`libs/` 里那些片
      // 是被引用的一方，单独拿它们跑一遍只会得到一句"这一句引的 libs/x 认不出"。
      const entries = sources.filter((s) => !s.name.includes('/') && isEntry(dir, s.name))
      const run1 = entries.map(run)
      if (g.bad) {
        const wanted = expectedMsgs(g)
        const got = run1
        const text = got.map((r) => r.out).join('\n')
        if (!got.some((r) => r.failed)) {
          bad.push({ file: relative(siteRoot, f), line: sources[0].line, out: '标了 nf-bad 的这段，编译器居然没拦' })
          continue
        }
        // 站上那句 `// 报：…` 要一整行等于编译器那句（前面那个"文件名 第几行第几列："不算在内）。
        // 只比"输出里出现这一段"的话，一句话的尾巴改了字没人报——读者从站上抄走的就是这一整句。
        // 消息里偶尔带上这个检查自己造的临时目录（互相引用那一条就写全路径），那一段去掉，它不是读者能看到的东西。
        const flat = dir.replaceAll('\\', '/')
        const said = text.split(/\r?\n/).map((l) => l
          .replaceAll('\\', '/')
          .replaceAll(flat + '/', '')
          .replace(/^.*? 第 \d+ 行第 \d+ 列：/, '')
          // 装载那一步（开容器、认包根）报的话只带文件名不带行列：那一句也是位置，
          // 站上写的 `// 报：` 不该把 `main.nf：` 抄进去——读者在自己的工程里看到的也是那一条前缀。
          .replace(/^.*?\.(nf|json)：/, '')
          .trim())
        for (const w of wanted) {
          if (!said.includes(w)) {
            bad.push({ file: relative(siteRoot, f), line: sources[0].line, out: `说好的报错「${w}」不是编译器那句完整的话，实际是：\n${text}` })
          }
        }
        continue
      }
      for (const s of entries) {
        const r = run(s)
        if (r.failed) {
          bad.push({ file: relative(siteRoot, f), line: s.line, out: r.out })
          break
        }
      }
    }
  }
  return { n, bad }
}

// ---------- 三、导航与侧栏的链接是否指向存在的页面 ----------

// 死链检查只看 markdown 正文里的链接，`config.mts` 里 nav／sidebar 的 link 它不管。
// 会漏检的形状：link 写着 '/learn/'、'/reference/'、'/stdlib/' 指向目录页，
// 而站上没有目录页（板块首页的真名是 /learn/021_index 这一类），各项检查全绿、点上去却 404。
// 这里把 config 里每条 link 都按"cleanUrls 开着的 VitePress 会怎么找文件"核一遍存在性。
function checkNavLinks() {
  const cfg = readFileSync(join(siteRoot, '.vitepress/config.mts'), 'utf8')
  const excluded = new Set(['README.md']) // config 的 srcExclude：这些文件根本不构建
  const checked = []
  const bad = []
  for (const m of cfg.matchAll(/link:\s*'([^']+)'/g)) {
    const link = m[1]
    if (/^(https?:|mailto:|#)/.test(link)) continue
    const line = cfg.slice(0, m.index).split('\n').length
    checked.push(link)
    const clean = link.replace(/^\/+/, '').split('#')[0]
    const rel = clean === '' ? 'index.md' : clean.replace(/\/$/, '') + '.md'
    const alt = clean === '' ? null : clean.replace(/\/$/, '') + '/index.md'
    const hit = existsSync(join(siteRoot, rel)) || (alt && existsSync(join(siteRoot, alt)))
    if (!hit || excluded.has(rel)) bad.push({ line, link, want: rel })
  }
  return { checked: checked.length, bad }
}

// ---------- 跑 ----------

const mdFiles = [...walk(siteRoot)]
const tmp = mkdtempSync(join(tmpdir(), 'nf-site-'))
try {
  const h = checkHighlight(mdFiles, tmp)
  const e = checkExamples(mdFiles, tmp)
  const v = checkNavLinks()

  console.log(`高亮对照：比了 ${h.checked} 段代码块`)
  for (const b of h.bad) {
    console.log(`  ✗ ${b.file}:${b.line}\n      站点 ${b.js}\n      编译器 ${b.go}`)
  }
  console.log(`例子检查：跑了 ${e.n} 组（nf 块要过检查，nf-bad 块要被拦）`)
  for (const b of e.bad) {
    console.log(`  ✗ ${b.file}:${b.line}\n${b.out.split('\n').map((l) => '      ' + l).join('\n')}`)
  }
  console.log(`导航与侧栏：比了 ${v.checked} 条 config 里的 link，${v.bad.length} 条站上没有页面`)
  for (const b of v.bad) {
    console.log(`  ✗ .vitepress/config.mts:${b.line}  link '${b.link}' → 站上找不到 ${b.want}`)
  }
  const fails = h.bad.length + e.bad.length + v.bad.length
  if (fails === 0) console.log('全部对齐：高亮器与编译器同一套记号，站上每个例子都过了编译器检查，每条导航都落在存在的页面上。')
  else console.log(`共 ${fails} 处不对。`)
  process.exitCode = fails === 0 ? 0 : 1
} finally {
  rmSync(tmp, { recursive: true, force: true })
}
