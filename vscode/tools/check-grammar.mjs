// check-grammar.mjs 核对三张表对齐：编译器词法层、这份 TextMate 语法、站上 `site/reference/034_syntax.md` 的保留名字一节。
//
// 做法与 site/tools-site/check-site.mjs 比对高亮器那一项同一思路：以真的 `nf tokens` 输出为判定依据。
//   1. 语法文件里的名字表（关键字与保留名）与站上那一节的名单逐字对齐，两个方向都比；
//      那句里的三个数（关键字、内建名称、合计）跟着核对。
//   2. 语法文件里的运算符表逐个送进 `nf tokens`，不识别的、写法不一致的当场报失败。
//   3. 运算符表必须长的排前面：TextMate 取先出现的分支，短的排前面会把 `<=` 切成 `<` 再加 `=`。
//
// 跑法：node tools/check-grammar.mjs（或 npm run check）
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const extRoot = resolve(here, '..')
const repoRoot = resolve(extRoot, '..')
const nfExe = join(repoRoot, process.platform === 'win32' ? 'nf.exe' : 'nf')

const fail = []
const bad = (msg) => fail.push(msg)

// 语法文件里那些 (?<![\w$])(a|b|c)(?![\w$]) 的形状，把每个纯标识符的分支收集起来。
function namesFromGrammar(grammar) {
  const out = new Set()
  const walk = (node) => {
    if (Array.isArray(node)) {
      node.forEach(walk)
      return
    }
    if (node && typeof node === 'object') {
      if (typeof node.match === 'string') {
        for (const m of node.match.matchAll(/\(([^()]*)\)/g)) {
          const parts = m[1].split('|')
          if (parts.every((p) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(p))) {
            parts.forEach((p) => out.add(p))
          }
        }
      }
      Object.values(node).forEach(walk)
    }
  }
  walk(grammar.repository)
  return out
}

// 运算符字段的分支逐个拆出来：`\|` 是运算符本身的竖线，只有没转义的竖线才是分隔符。
function opsFromGrammar(grammar) {
  const body = grammar.repository.operators.patterns[0].match.replace(/^\(/, '').replace(/\)$/, '')
  const parts = []
  let cur = ''
  for (let i = 0; i < body.length; i++) {
    if (body[i] === '\\') {
      cur += body[i] + body[i + 1]
      i++
      continue
    }
    if (body[i] === '|') {
      parts.push(cur)
      cur = ''
      continue
    }
    cur += body[i]
  }
  parts.push(cur)
  return parts.map((p) => p.replace(/\\(.)/g, '$1'))
}

// 站上 `site/reference/034_syntax.md` 的"保留名字"一节是这张表面向用户的出处。
function namesFromSite() {
  const page = readFileSync(join(repoRoot, 'site', 'reference', '034_syntax.md'), 'utf8')
  const section = page.split(/^## /m).find((s) => s.startsWith('保留名字'))
  if (!section) {
    bad('站上 034_syntax.md 里没有"保留名字"那一节，这张对照没法做')
    return null
  }
  const rows = [...section.matchAll(/^\| (.+?) \| (.+)\|$/gm)]
  const kwRow = rows.find((r) => r[1].trim() === '关键字')
  const counts = section.match(/(\d+) 个关键字，加上 (\d+) 个内建名称，一共 (\d+) 个词/)
  if (!kwRow || !counts) {
    bad(`站上 034_syntax.md 那一节读不出关键字那一行或那三个数（行 ${Boolean(kwRow)}、数 ${Boolean(counts)}），这张对照没法做`)
    return null
  }
  const words = (cell) => [...cell.matchAll(/`([^`]+)`/g)].flatMap((m) => m[1].trim().split(/\s+/))
  const keywords = new Set(words(kwRow[2]))
  const reserved = new Set()
  for (const r of rows) {
    if (r === kwRow || r[1].trim() === '类别') continue
    for (const w of words(r[2])) {
      if (!keywords.has(w)) reserved.add(w)
    }
  }
  return { keywords, reserved, listed: Number(counts[1]), reservedCount: Number(counts[2]), total: Number(counts[3]) }
}

// 拿 nf tokens 把一份探针文件切成记号，只收 name 与 op 两类。
function lexedAs(file) {
  const raw = execFileSync(nfExe, ['tokens', file], { encoding: 'utf8' }).replace(/\r/g, '')
  const names = new Set()
  const ops = new Set()
  for (const line of raw.split('\n')) {
    const m = /^(name|op):(.*)$/.exec(line)
    if (!m) continue
    ;(m[1] === 'name' ? names : ops).add(m[2])
  }
  return { names, ops }
}

const grammar = JSON.parse(readFileSync(join(extRoot, 'syntaxes', 'nf.tmLanguage.json'), 'utf8'))
const gNames = namesFromGrammar(grammar)
const gOps = opsFromGrammar(grammar)

const fromSite = namesFromSite()
if (fromSite) {
  const expected = new Set([...fromSite.keywords, ...fromSite.reserved])
  const missing = [...expected].filter((w) => !gNames.has(w))
  const extra = [...gNames].filter((w) => !expected.has(w))
  if (missing.length) bad(`语法文件漏了这些名字：${missing.join(' ')}`)
  if (extra.length) bad(`语法文件里多出不认识的名字：${extra.join(' ')}`)
  if (fromSite.keywords.size !== fromSite.listed) {
    bad(`站上那一节写关键字 ${fromSite.listed} 个，格子里实际列了 ${fromSite.keywords.size} 个`)
  }
  if (fromSite.reserved.size !== fromSite.reservedCount) {
    bad(`站上那一节写内建名称 ${fromSite.reservedCount} 个，格子里实际列了 ${fromSite.reserved.size} 个`)
  }
  if (expected.size !== fromSite.total) {
    bad(`站上那一节写合计 ${fromSite.total}，两张名单并起来是 ${expected.size} 个`)
  }
}

for (let i = 1; i < gOps.length; i++) {
  if (gOps[i].length > gOps[i - 1].length) {
    bad(`运算符表第 ${i + 1} 个 ${gOps[i]} 比前一个 ${gOps[i - 1]} 长：长的必须排前面`)
    break
  }
}

const tmp = mkdtempSync(join(tmpdir(), 'nf-grammar-'))
try {
  const probe = join(tmp, 'probe.nf')
  writeFileSync(probe, [...gNames].join(' ') + '\n' + gOps.join(' ') + '\n')
  const real = lexedAs(probe)
  const notName = [...gNames].filter((w) => !real.names.has(w))
  const notOp = gOps.filter((o) => !real.ops.has(o))
  if (notName.length) bad(`这些名字在 nf 的词法层里不是记号：${notName.join(' ')}`)
  if (notOp.length) bad(`这些运算符 nf 词法层认不出，或写法不同：${notOp.join(' ')}`)
  const strayOps = [...real.ops].filter((o) => !gOps.includes(o))
  if (strayOps.length) bad(`nf 把这些也当运算符，语法文件里没有：${strayOps.join(' ')}`)
} finally {
  rmSync(tmp, { recursive: true, force: true })
}

if (fail.length) {
  console.error(`nf 语法表对账不通过（${fail.length} 条）：`)
  for (const f of fail) console.error('  ' + f)
  process.exit(1)
}
console.log(`nf 语法表对账通过：名字 ${gNames.size} 个、运算符 ${gOps.length} 个，与 nf tokens 同一套`)
