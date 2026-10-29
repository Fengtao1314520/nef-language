// 站上"一个名字贯穿三处"的自动检查：页面一级标题、侧栏与导航的 text、正文里的链接文字。
//
// 规矩：
//   1. 侧栏条目与正文链接的文字必须等于目标页面的一级标题；教程那一栏允许"第 N 课："前缀。
//   2. 顶部导航那六个板块名（入门指南／语法参考／标准库／库／工具与调试／关于）说的是板块，
//      不要求等于某一页的标题；正文里用同样的板块名指向同样的目标，算同一种写法，放行。
//
// 跑法：node tools-site/names.mjs   （或 npm run names）
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')
const configSrc = readFileSync(join(siteRoot, '.vitepress/config.mts'), 'utf8')

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === '.vitepress' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else if (name.endsWith('.md')) yield p
  }
}
const relOf = (p) => relative(siteRoot, p).split(sep).join('/')

// 页面一级标题，键用站内的链接写法：/learn/021_index
const title = {}
let pageCount = 0
for (const f of walk(siteRoot)) {
  const r = relOf(f)
  if (r === 'README.md') continue
  const m = /^# (.+)$/m.exec(readFileSync(f, 'utf8'))
  if (!m) {
    // 首页是 home 布局，没有一级标题，标题位由 frontmatter 的 hero 承担
    if (r !== 'index.md') {
      console.log(`✗ ${r} 没有一级标题`)
      process.exitCode = 1
    }
    continue
  }
  title['/' + r.replace(/\.md$/, '')] = m[1].trim()
  if (r.endsWith('index.md')) title['/' + r.replace(/index\.md$/, '')] = m[1].trim()
  pageCount++
}

// 课程那一栏允许"第 N 课："前缀
const coursePrefix = /^第 \d+ 课：/

// text 与目标页标题算不算同一个名字
function matches(text, link) {
  const t = title[link] ?? title[link + '/index']
  if (!t) return { ok: false, why: '站上没有这份页面' }
  if (text === t) return { ok: true }
  const prefix = text.match(coursePrefix)
  if (prefix && text.slice(prefix[0].length) === t) return { ok: true }
  return { ok: false, why: `页面一级标题是"${t}"` }
}

// 顶部导航里的板块名与它指向的页面：下拉那一栏算它下面几条。
// 正文里用同样的板块名指向同一个板块的任意一页，放行。
const navBlock = configSrc.slice(configSrc.indexOf('nav: ['), configSrc.indexOf('sidebar: {'))
const sections = (() => {
  const out = []
  let depth = 0
  let cur = null
  for (const m of navBlock.matchAll(/\{|\}|text:\s*'([^']+)'|link:\s*'([^']+)'/g)) {
    if (m[0] === '{') {
      depth++
      if (depth === 1) cur = { text: null, links: [] }
      continue
    }
    if (m[0] === '}') {
      if (depth === 1 && cur && cur.text) out.push(cur)
      if (depth === 1) cur = null
      depth--
      continue
    }
    if (!cur) continue
    if (m[1] !== undefined && depth === 1 && !cur.text) cur.text = m[1]
    if (m[2] !== undefined) cur.links.push(m[2])
  }
  return out
})()
const isSectionLabel = (text, link) => sections.some((s) => s.text === text && s.links.includes(link))

let bad = 0
let checked = 0
const report = (where, text, link, why) => {
  bad++
  console.log(`✗ ${where}\n    链接文字："${text}"　目标：${link}\n    ${why}`)
}

// 1. 侧栏条目：{ text, link } 且 link 不是板块首页之外的导航写法都按同一标题要求核
for (const m of configSrc.matchAll(/\{\s*text:\s*'([^']+)',\s*link:\s*'([^']+)'/g)) {
  const [, text, link] = m
  const inNav = configSrc.slice(0, m.index).includes('nav: [') && !configSrc.slice(0, m.index).includes('sidebar: {')
  if (inNav) continue
  checked++
  const r = matches(text, link)
  if (!r.ok) report(`config.mts 侧栏条目 ${link}`, text, link, r.why)
}

// 2. 正文里的站内链接
for (const f of walk(siteRoot)) {
  const rel = relOf(f)
  if (rel === 'README.md') continue
  let src = readFileSync(f, 'utf8')
  src = src.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, '').replace(/```[\s\S]*?```/g, '')
  const lines = src.split(/\r?\n/)
  lines.forEach((line, i) => {
    for (const m of line.matchAll(/(!?)\[([^\]]+)\]\((\/[^\s)]+)\)/g)) {
      if (m[1] === '!') continue
      const [, , textRaw, link] = m
      const text = textRaw.replace(/\*\*/g, '').trim()
      if (!title[link] && !title[link + '/index']) {
        report(`${rel}:${i + 1}`, text, link, '站上没有这份页面')
        continue
      }
      if (isSectionLabel(text, link)) continue
      checked++
      const r = matches(text, link)
      if (!r.ok) report(`${rel}:${i + 1}`, text, link, `${r.why}（板块名那种写法要求与顶部导航的 text 与 link 同时一致）`)
    }
  })
}

if (bad) {
  console.log(`\n名字检查：${bad} 处不是同一个名字。`)
  process.exitCode = 1
} else {
  console.log(`名字检查：${pageCount} 份页面的标题、${sections.length} 条板块名、比了 ${checked} 处侧栏条目与正文链接，全部同一个名字。`)
}
