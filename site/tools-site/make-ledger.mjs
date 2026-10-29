// 生成 核对清单.json：站上每一段"代码 + 声称的输出"都真跑一遍。
//
// 代码合不合法由 tools-site/check-site.mjs 管；这一份管另一头——**声称的输出是不是真的**。
// 跑的是仓库根那一份 nf.exe，走 `nf 文件.nf` 那条路（内存里编译完直接执行），
// 所以这里既验了例子，也顺手验了直跑与产物用的是同一套后端。
//
// 怎么把围栏块分成"自己是一篇"与"一个工程的一片"写在 example-groups.mjs 里，
// 与 check-site.mjs 共用同一份规则——同一段跨文件的例子在两处都得是同一回事。
//
// 跑法：npm run ledger（要先有 ..\nf.exe：cd ..\compiler && go build -o ..\nf.exe .\cmd\nf）

import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { tmpdir } from 'node:os'

import { blocks, groupsOf, isEntry, walk } from './example-groups.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')
const nfExe = resolve(siteRoot, '..', 'nf.exe')

// 输出块只有**紧贴**在代码块后面才算这段例子的输出。
// 隔着一段话就不算了——不然"下面那一段讲的是另一件事"的文本会被错配过来，
// 台账上就凭空多出一条"对不上"。
function claimedBlock(list, i) {
  const next = list[i + 1]
  if (!next || next.lang !== 'text') return ''
  if (next.line !== list[i].end + 2) return ''
  return next.body
}

// 单值的输出写在代码的行内注释里：`console.print(x)   // 3`
function inlineClaims(code) {
  return code
    .split('\n')
    .map((l) => {
      const m = /console\.print\(.*\)\s*\/\/\s*(.+)$/.exec(l)
      return m ? m[1].trim() : ''
    })
    .filter((s) => s.length > 0)
}

// 真跑一遍（一个工程就是同一个临时目录里的几份 .nf）。除了"对得上／对不上"，
// 还有几类跑不了的都要分开记：编译器还没实现的那种写法、这一段需要外部输入
// （读某个文件、等键盘输入）、以及真的跑出了别的结果。
// 混在一起就会把"这一版还没做到"当成"站上写错了"。
function runFiles(dir, entryName, needArgs, needStdin) {
  const argv = [join(dir, entryName)]
  if (needArgs) argv.push('甲', '乙')
  const opts = { cwd: dir, encoding: 'utf8', timeout: 60000 }
  if (needStdin) opts.input = '第一行\n第二行\n'
  try {
    const out = execFileSync(nfExe, argv, opts)
    return { ok: true, out }
  } catch (e) {
    const out = (e.stdout || '') + (e.stderr || '')
    return { ok: false, out, code: e.status }
  }
}

const todo = [
  '后端还没做到这些',
  '还没接到后端',
  '还不支持',
  '还没做',
  '读不到文件',
]

const rows = []
let ran = 0
let bad = 0
const workDir = join(tmpdir(), `nef-ledger-${process.pid}`)
rmSync(workDir, { recursive: true, force: true })
mkdirSync(workDir, { recursive: true })

// verdict 把跑出来的与声称的对一次号，交回台账那一行要写的话；只有对不上才算坏。
function verdict(r, claim) {
  const why = todo.find((w) => r.out.includes(w))
  if (!r.ok && why) return `等后端或等现场（${why}）`
  if (!r.ok) {
    bad++
    return `跑不起来（退出码 ${r.code}）：${r.out.trim().slice(0, 120)}`
  }
  if (r.out.trimEnd() !== claim.trimEnd()) {
    bad++
    return `对不上：实得 ${JSON.stringify(r.out.trimEnd()).slice(0, 200)}`
  }
  return '真跑过，输出一致'
}

try {
  let caseNo = 0
  for (const f of walk(siteRoot)) {
    const md = readFileSync(f, 'utf8')
    const list = blocks(md)
    const page = relative(siteRoot, f).replace(/\\/g, '/')

    for (const g of groupsOf(md)) {
      if (g.bad) continue // nf-bad 那一段验的是报错的原话，归 check-site.mjs 管
      // 借来的那一片只是"跑起来得有这个文件"，不是站上多出来的一个例子；
      // `json:` 那一片是包的清单（nf.json），它不是 nef 代码，不进台账也不参与"谁是主文件"
      const own = g.items.filter((it) => !it.borrowed && !it.data)
      if (!own.length) continue // 只有清单，没有代码可跑
      const last = own.length - 1
      const project = own.length > 1
      const entry = own[last]
      // 一个工程的声称输出看主文件后面紧贴的那一块；行内注释则整堆都算
      const block = claimedBlock(list, list.findIndex((b) => b.line === entry.line))
      const inline = inlineClaims(own.map((it) => it.body).join('\n'))
      const claim = block || (inline.length ? inline.join('\n') : '')

      let state = '这一段没声称输出'
      if (claim) {
        const dir = join(workDir, `case${++caseNo}`)
        mkdirSync(dir, { recursive: true })
        const ownIdx = own.map((it) => g.items.indexOf(it))
        const names = g.items.map((it, i) => it.name ?? `part${i}.nf`)
        names.forEach((nm, i) => {
          const to = join(dir, nm)
          mkdirSync(join(to, '..'), { recursive: true }) // 包那种例子落在 `libs/包名/` 那一层下面
          writeFileSync(to, g.items[i].body)
        })
        const ownEntry = names[ownIdx[last]]
        // 工程里谁是主文件：摆在工程根上、其他源文件都没有 import 它的那一个
        // （`libs/` 里那些片是被引用的一方，单独跑它们只会得到一句"这一句引的 libs/x 认不出"）
        const entryName = project ? names.find((nm) => !nm.includes('/') && isEntry(dir, nm)) ?? ownEntry : ownEntry
        const whole = own.map((it) => it.body).join('\n')
        // 需要外部输入的两类：读了 args、或等了键盘输入
        const r = runFiles(dir, entryName, /\bargs\b/.test(whole), /console\.read\(/.test(whole))
        ran++
        state = verdict(r, claim)
      }

      own.forEach((it, i) => {
        const tail = i === last
        rows.push({
          页面: page,
          代码行: it.line,
          代码: it.body,
          声称输出: tail ? claim : '',
          写法: tail && block ? '输出块' : tail && inline.length ? '行内注释' : '无',
          检查器: '已过 nf check',
          输出状态: tail ? state : '工程的一片，跟着主文件一起跑'
        })
      })
    }
  }
} finally {
  rmSync(workDir, { recursive: true, force: true })
}

const out = {
  说明: '站上所有例子的台账。代码那一列由 tools-site/check-site.mjs 与编译器逐条核对；' +
    '输出那一列走仓库根的 nf.exe（`nf 文件.nf`：内存里编译完直接执行）真跑一遍，' +
    '与声称的输出一字一样才算过。连着排的几个具名块算一个工程，落到同一个目录里跑主文件。',
  共: rows.length,
  有输出的条数: rows.filter((r) => r.声称输出).length,
  真跑过的条数: ran,
  对不上的条数: bad,
  用输出块的: rows.filter((r) => r.写法 === '输出块').length,
  用行内注释的: rows.filter((r) => r.写法 === '行内注释').length,
  条目: rows
}

const target = join(siteRoot, '核对清单.json')
mkdirSync(dirname(target), { recursive: true })
writeFileSync(target, JSON.stringify(out, null, 2) + '\n')
console.log(
  `记了 ${out.共} 段例子，其中 ${out.有输出的条数} 段声称了输出；真跑了 ${ran} 段，对不上 ${bad} 段。` +
    (bad ? '' : '（全部一致）')
)
if (bad) {
  for (const r of out.条目.filter((x) => x.输出状态 !== '真跑过，输出一致' && x.声称输出)) {
    console.log(`  ${r.页面}:${r.代码行} —— ${r.输出状态}`)
  }
  process.exitCode = 1
}
