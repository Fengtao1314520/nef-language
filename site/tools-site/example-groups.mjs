// example-groups.mjs 是站上示例的分组规则，唯一定义处。
//
// 单独成一份的理由：`check-site.mjs`（代码过不过编译器）与 `make-ledger.mjs`（声称的输出
// 是不是真跑出来的）都得先把围栏块还原成"一篇独立的例子"或"一个多文件工程"。同一件事写两份
// 会不一致：台账一度把带 import 的例子一律记成"跨文件的例子，单独一段跑不了"，
// 而检查那一头已经在合成工程里跑。
//
// 规则（与站上写多文件例子的约定配合：`import` 只认 / 且大小写敏感）：
// - 块顶部写 `// xxx.nf` 的，是某个工程的一片；
// - 连着排的几个具名块算同一个工程；**名字相同就在此处另起一个工程**——站上两份连着排的工程
//   常常都拿 `main.nf` 当主文件，不切开就会让后一份的 `main.nf` 盖掉前一份的；
// - 具名块后面紧跟的那一个无名块，是这个工程的主文件，也是这一组的收尾；
// - 其余每个块自己是一篇；
// - 收尾之后再来具名块是下一份工程开始了，但它 `import` 的名字若在前一份里出现过，
//   就把那一片**借**过来一起落盘（只借不搬走，两份例子都要能各自跑）。
//
// 带包的那种例子多一条：`nf.json` 里不许写注释（读它的是 json.Unmarshal），所以那一片的名字
// 不能像 `.nf` 那样写在块顶上，只能挂在围栏上——围栏写 `json libs/greet/nf.json`，高亮识别的
// 语言还是 `json`，名字是第二个记号，渲染出来看不见。它跟着前面那一片落进同一个工程；
// 排在最前（先展示清单再展示代码）时自成一组，后面那一片代码并入这一组，这一组是"通过检查"
// 还是"被拒绝"由那一片代码决定。

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

// blocks 切出一篇 markdown 里所有围栏块：lang 是围栏上那个记号，file 是它后面第二个记号
// （只有 `json libs/greet/nf.json` 这一种用到它），line/end 是行号（一号起）。
// body 尾巴上那个换行原样留着——高亮对照那一条比的就是带换行的那一份。
export function blocks(md) {
  const out = []
  const lines = md.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const m = /^```(\S*)(?:\s+(\S*))?\s*$/.exec(lines[i])
    if (!m) continue
    const body = []
    let j = i + 1
    while (j < lines.length && !/^```\s*$/.test(lines[j])) { body.push(lines[j]); j++ }
    out.push({ lang: m[1], file: m[2] ?? '', body: body.join('\n') + '\n', line: i + 1, end: j + 1 })
    i = j
  }
  return out
}

// importsOf 交回这个块 import 了哪些文件名（`import { a } from "./x.nf"` 里那个 x.nf）。
function importsOf(body) {
  const out = []
  for (const m of body.matchAll(/^\s*import\s[^\n]*?from\s+"([^"]+)"/gm)) {
    out.push(m[1].replace(/^(\.\/)+/, ''))
  }
  return out
}

export function groupsOf(md) {
  const groups = []
  let open = null
  for (const b of blocks(md)) {
    if (b.lang === 'json' && /^[\w./-]+\.json$/.test(b.file)) {
      const item = { name: b.file, body: b.body, line: b.line, data: true }
      if (open && !open.closed) {
        open.items.push(item)
      } else {
        open = { bad: null, closed: false, items: [item] }
        groups.push(open)
      }
      continue
    }
    if (b.lang !== 'nf' && b.lang !== 'nf-bad') continue
    const bad = b.lang === 'nf-bad'
    const m = /^\s*\/\/\s*([\w./-]+\.nf)\b/.exec(b.body)
    if (m) {
      const joins = open && (open.bad === null || open.bad === bad) && !open.closed && !open.items.some((it) => it.name === m[1])
      if (joins) {
        if (open.bad === null) open.bad = bad
        open.items.push({ name: m[1], body: b.body, line: b.line })
      } else {
        open = { bad, closed: false, items: [{ name: m[1], body: b.body, line: b.line }] }
        groups.push(open)
      }
      continue
    }
    if (/^\s*\/\/\s*$/.test(b.body)) continue // 空例子不参与
    if (open && !open.closed && open.items.length > 0 && (open.bad === null || open.bad === bad)) {
      if (open.bad === null) open.bad = bad
      open.closed = true
      open.items.push({ name: 'entry.nf', body: b.body, line: b.line })
      continue
    }
    groups.push({ bad, closed: true, items: [{ name: null, body: b.body, line: b.line }] })
  }

  // 借：这一组 import 了某个名字，而这个名字在其他组里出现过，就把那一片复制进来。
  // 复制不搬走——两边各跑各的。反复补齐直到不再新增（借来的那一片自己也可能 import）。
  for (const g of groups) {
    for (let pass = 0; pass < 4; pass++) {
      const missing = []
      for (const it of [...g.items]) {
        for (const nm of importsOf(it.body)) {
          if (!g.items.some((x) => x.name === nm) && !missing.includes(nm)) missing.push(nm)
        }
      }
      if (missing.length === 0) break
      let got = false
      for (const nm of missing) {
        const src = groups.find((o) => o !== g && o.bad === g.bad && o.items.some((it) => it.name === nm))
        if (!src) continue
        const it = src.items.find((x) => x.name === nm)
        g.items.unshift({ name: it.name, body: it.body, line: it.line, borrowed: true })
        got = true
      }
      if (!got) break
    }
  }
  return groups
}

// isEntry 识别主文件：目录里其他源文件都没有 import 它，它才是入口。
// 逐层都看（包那种例子里 `libs/greet/greet.nf` 会引自己包内的另一片），只比 `.nf`——
// `nf.json` 是清单，不引用任何文件。
export function isEntry(dir, name) {
  for (const rel of sources(dir)) {
    if (rel === name) continue
    const src = readFileSync(join(dir, rel), 'utf8')
    if (src.includes(`"${name}"`) || src.includes(`"./${name}"`)) return false
  }
  return true
}

function sources(dir, prefix = '') {
  const out = []
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.isDirectory()) out.push(...sources(join(dir, e.name), prefix + e.name + '/'))
    else if (e.isFile() && e.name.endsWith('.nf')) out.push(prefix + e.name)
  }
  return out
}

// walk 收站点目录里所有 markdown，跳过依赖与隐藏目录。
export function* walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === '.vitepress' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else if (p.endsWith('.md')) yield p
  }
}
