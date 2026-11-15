// 站点的五条硬检查：
//   1. 高亮器与编译器的词法必须认同一套记号；
//   2. 站上每个 ```nf 代码块，都得能通过编译器的检查；
//   3. 每个 ```nf-bad 块（示范错误写法）必须真的被拒绝，且报出作者预期的那句话；
//   4. config 里每一条 link 都要对上一个真实存在的页面；
//   5. 站上写死的数字（关键字、Facts 字段、组名动作）与源码逐项对得上，
//      且已经删掉的旧说法（"只读引用"）一处都不许回来。
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

// 与 nf.exe tokens 的比对标准：关键字与保留名都算 name；注释不进比较；
// 字符串与字符只比类型——编译器返回的是解码后的内容，高亮器看到的是原文。
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

// ---------- 四、数字与源码、文档三处对齐 ----------

// 站上写死的几个数：关键字与内建名称在语法速查与首页，标准库成员数在总览页，
// Facts 的字段数与起始行在设计取舍页。源码那一侧各有一个唯一的出处：
// 关键字表在 internal/lexer 的 init（两串 strings.Fields），Facts 是 check.go 里的结构体，
// 组名动作是 check/expr.go 的 stdFuncs。改了一处而忘了另一处，读者看到的数字就是错的。

const NL = String.fromCharCode(10)
const CR = String.fromCharCode(13)
const TAB = String.fromCharCode(9)

function linesOf(text) {
  return text.split(NL)
}

function readRepo(rel) {
  return readFileSync(join(repoRoot, rel), 'utf8').split(CR).join('')
}

// keywordsIn 数 internal/lexer 里那两串 strings.Fields：第一串是关键字，第二串是内建名称。
function keywordsIn(src) {
  const out = []
  for (const m of src.matchAll(/strings\.Fields\(([\s\S]*?)\)\s*\{/g)) {
    const words = [...m[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]).join(' ').split(/\s+/).filter(Boolean)
    out.push(words.length)
  }
  return out
}

// factsShape 找 check.go 的 `type Facts struct {` 起，数到那一个单独的 `}`：
// 字段只数导出的（大写开头），起始行是 1 起算的行号。
function factsShape(src) {
  const lines = linesOf(src)
  const at = lines.findIndex((l) => l.startsWith('type Facts struct {'))
  if (at < 0) return null
  const fields = []
  for (let i = at + 1; i < lines.length; i++) {
    if (lines[i] === '}') break
    if (!lines[i].startsWith(TAB)) continue
    const m = /^([A-Z][A-Za-z0-9_]*)\s+/.exec(lines[i].slice(1))
    if (m) fields.push(m[1])
  }
  return { start: at + 1, count: fields.length, fields }
}

// stdFuncCount 数 stdFuncs 那张表里的 `"组名.动作":` 条目。
function stdFuncCount(src) {
  const at = src.indexOf('var stdFuncs = map[string]stdSig{')
  if (at < 0) return -1
  const end = src.indexOf(NL + '}' + NL, at)
  const body = src.slice(at, end)
  return linesOf(body).filter((l) => l.startsWith(TAB) && /^"[a-z_]+\.[a-z_0-9]+":/.test(l.slice(1))).length
}

function checkCounts() {
  const [keywords, reserved] = keywordsIn(readRepo('compiler/internal/lexer/lexer.go'))
  const facts = factsShape(readRepo('compiler/internal/check/check.go'))
  const actions = stdFuncCount(readRepo('compiler/internal/check/expr.go'))

  const syntax = readFileSync(join(siteRoot, 'reference/034_syntax.md'), 'utf8')
  const index = readFileSync(join(siteRoot, 'index.md'), 'utf8')
  const stdlib = readFileSync(join(siteRoot, 'stdlib/044_index.md'), 'utf8')
  const design = readFileSync(join(siteRoot, 'about/064_design.md'), 'utf8')

  const bad = []
  const same = (file, what, doc, want) => {
    if (doc !== want) bad.push({ file, what, doc, want })
  }

  // 语法速查那一句同时写着关键字、内建名称与两者之和。
  const mKw = /(\d+) 个关键字，加上 (\d+) 个内建名称，一共 (\d+) 个词不能用作/.exec(syntax)
  if (!mKw) {
    bad.push({ file: 'reference/034_syntax.md', what: '关键字那一句', doc: '没找到', want: `${keywords} + ${reserved}` })
  } else {
    same('reference/034_syntax.md', '关键字个数', Number(mKw[1]), keywords)
    same('reference/034_syntax.md', '内建名称个数', Number(mKw[2]), reserved)
    same('reference/034_syntax.md', '两者之和', Number(mKw[3]), keywords + reserved)
  }
  const mIndex = /(\d+) 个关键字、/.exec(index)
  if (!mIndex) bad.push({ file: 'index.md', what: '首页那句关键字个数', doc: '没找到', want: keywords })
  else same('index.md', '关键字个数', Number(mIndex[1]), keywords)

  const mActions = /写成 `组名\.动作` 的一共 (\d+) 个/.exec(stdlib)
  if (!mActions) bad.push({ file: 'stdlib/044_index.md', what: '组名动作那一句', doc: '没找到', want: actions })
  else same('stdlib/044_index.md', '组名动作个数', Number(mActions[1]), actions)

  const mFacts = /check\.go` 第 (\d+) 行起，共 (\d+) 个字段/.exec(design)
  if (!mFacts) {
    bad.push({ file: 'about/064_design.md', what: 'Facts 那一句', doc: '没找到', want: `${facts?.start}/${facts?.count}` })
  } else if (!facts) {
    bad.push({ file: 'compiler/internal/check/check.go', what: 'Facts 结构体', doc: '没找到', want: 'type Facts struct' })
  } else {
    same('about/064_design.md', 'Facts 起始行', Number(mFacts[1]), facts.start)
    same('about/064_design.md', 'Facts 字段数', Number(mFacts[2]), facts.count)
  }

  // 已经删掉的旧说法：站上的散文里一处都不许回来（术语统一成"只读借用"）。
  for (const f of walk(siteRoot)) {
    const text = readFileSync(f, 'utf8').split(CR).join('')
    linesOf(text).forEach((line, i) => {
      if (line.includes('只读引用')) bad.push({ file: relative(siteRoot, f), line: i + 1, what: '旧说法"只读引用"', doc: '出现了', want: '一处都不该有' })
    })
  }
  return { keywords, reserved, actions, facts, bad }
}

// ---------- 跑 ----------

const mdFiles = [...walk(siteRoot)]
const tmp = mkdtempSync(join(tmpdir(), 'nf-site-'))
try {
  const h = checkHighlight(mdFiles, tmp)
  const e = checkExamples(mdFiles, tmp)
  const v = checkNavLinks()
  const c = checkCounts()

  console.log(`高亮对照：比了 ${h.checked} 段代码块`)
  for (const b of h.bad) {
    console.log(`  ✗ ${b.file}:${b.line}\n      站点 ${b.js}\n      编译器 ${b.go}`)
  }
  console.log(`例子检查：跑了 ${e.n} 组（nf 块要过检查，nf-bad 块要被拦）`)
  for (const b of e.bad) {
    console.log(`  ✗ ${b.file}:${b.line}\n${b.out.split('\n').map((l) => '      ' + l).join('\n')}`)
  }
  console.log(`导航与侧栏：比了 ${v.checked} 条 config 里的 link，${v.bad.length} 条站上没有页面`)
  console.log(`数字核对：关键字 ${c.keywords} 个、内建名称 ${c.reserved} 个、组名动作 ${c.actions} 个、Facts ${c.facts ? c.facts.count + ' 个字段（' + c.facts.start + ' 行起）' : '没读到'}`)
  for (const b of c.bad) {
    const at = b.line ? `:${b.line}` : ''
    console.log(`  ✗ ${b.file}${at}  ${b.what}：文档 ${b.doc}，源码 ${b.want}`)
  }
  for (const b of v.bad) {
    console.log(`  ✗ .vitepress/config.mts:${b.line}  link '${b.link}' → 站上找不到 ${b.want}`)
  }
  const fails = h.bad.length + e.bad.length + v.bad.length + c.bad.length
  if (fails === 0) console.log('全部对齐：高亮器与编译器同一套记号，站上每个例子都过了编译器检查，每条导航都落在存在的页面上，写死的数字与源码逐项一致。')
  else console.log(`共 ${fails} 处不对。`)
  process.exitCode = fails === 0 ? 0 : 1
} finally {
  rmSync(tmp, { recursive: true, force: true })
}
