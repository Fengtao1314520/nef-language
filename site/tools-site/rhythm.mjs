// 量每页的写作节奏：段落长短是否交错、有没有一句成段、表格是否过多、
// 是否踩了 AI 套话黑名单。这份表是文案口径的唯一出处，原先抄它的
// 站点文风指南与其余写站指南都已删除，改口径改这里。
//
// 跑法：node tools-site/rhythm.mjs   （或 npm run rhythm）

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')

const banned = [
  '值得一提', '需要注意的是', '值得注意', '总的来说', '综上', '换句话讲', '换句话说', '在实际开发中',
  '我们推荐', '不仅', '无论是', '让你专注', '本质上', '归根结底', '简言之', '简单来说',
  '强大', '优雅', '简洁高效', '一目了然', '清晰明了', '轻松', '极致', '开箱即用', '所见即所得',
  '一站式', '深度定制', '高度可定制', '完善的', '丰富的', '赋能',
  '收尾', '抓手', '落地', '闭环',
  '拦下来', '紧挨着用', '操心', '试试看', '带走两句', '痛点', '房产',
  '你', '撞', '清单', '那一格', '空出一格', '格子',
  '常见陷阱', '注意事项', '新手容易犯的错误', 'API文档', '参考手册', '语法糖',
  '首先，', '其次，', '综上所述', '值得注意的是',
  '这一条挡住的是', '代价是另一种自觉', '理由写在规范里', '值得记住的是',
  // 下面这一批是仓库内部说惯了的说法，读者看不懂那一句在讲什么；已逐处改掉，钉在这里防回流。
  // 报错原文写在行内代码里，`proseOnly()` 早把行内代码剥掉，所以这些词出现在引用编译器原话的那一处不算违规。
  '钉住', '挂上', '挂到', '借看', '只借不交', '写代码的人', '盖穿', '不太顺手', '说清', '讲清', '站上',
  // 互联网黑话一批（站上一个都没有，钉在这里防回潮）。
  // 对齐、布局、矩阵、生态、体系这几个词在本页另有真实技术含义（栈对齐、内存布局），所以不进这张表，逐处人工判断。
  '沉淀', '拉通', '打通', '协同', '联动', '洞察', '赛道', '心智', '调性', '战役', '链路', '势能',
  '兜底', '落盘', '收口', '透传', '口径', '对标', '触点', '卡点',
  '§', '规范', 'SPEC', '待裁',
]

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === '.vitepress' || name.startsWith('.')) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) yield* walk(p)
    else if (p.endsWith('.md')) yield p
  }
}

// 只取正文段落：跳过代码块、表格、标题、列表、提示块
function paragraphs(md) {
  const out = []
  let inFence = false
  let cur = []
  // 文首的 frontmatter 是给主题看的字段，不是读者段落，不参与节奏统计
  const lines = md.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, '').split('\n')
  const flush = () => {
    if (cur.length) { out.push(cur.join('')); cur = [] }
  }
  for (const line of lines) {
    if (/^```/.test(line)) { inFence = !inFence; flush(); continue }
    if (inFence) { flush(); continue }
    if (/^(#|\||>|```|\s*[-*]\s|\s*$|---)/.test(line)) { flush(); continue }
    cur.push(line.trim())
    if (/[。？！：;]$/.test(line.trim())) flush()
  }
  flush()
  // 行内代码只去掉反引号本身，内容照常计字——读者是真要读这些字符的。
  // 链接只算看得见的字面：URL 读者不读，不能把它算进段落长度。
  return out.map((p) => p.replace(/`/g, '').replace(/\*\*/g, '').replace(/\[([^[\]]*)\]\([^)]*\)/g, '$1'))
}

// 黑名单只看正文：代码块和行内代码里的字面量（路径、示例文件名）不算套话
function proseOnly(md) {
  return md.replace(/```[\s\S]*?```/g, '').replace(/`[^`\n]*`/g, '')
}

function stats(md) {
  const ps = paragraphs(md)
  const lens = ps.map((p) => [...p].length).filter((n) => n > 0).sort((a, b) => a - b)
  const med = lens.length ? lens[Math.floor(lens.length / 2)] : 0
  const short = lens.filter((n) => n <= 40).length
  const longOnes = lens.filter((n) => n > 140).length
  const oneSentence = ps.filter((p) => [...p].length <= 22 && /[。！？：]$/.test(p)).length
  const tableRows = (md.match(/^\|[-\s|:]*\|$/gm) ? 0 : 0) + md.split('\n').filter((l) => /^\|/.test(l)).length
  const callouts = (md.match(/^>\s/gm) || []).length
  const flatNeighbors = lens.filter((n, i) => i > 0 && Math.abs(n - lens[i - 1]) / Math.max(n, lens[i - 1]) < 0.2).length
  const hits = banned.filter((w) => proseOnly(md).includes(w))
  const women = (md.match(/我们/g) || []).length
  return {
    段数: ps.length, 中位: med, 短段比: ps.length ? Math.round(100 * short / ps.length) : 0,
    长段: longOnes, 一句成段: oneSentence, 表格行: tableRows, 提示块: callouts,
    等长相邻: flatNeighbors, 黑名单: hits, 我们: women
  }
}

const rows = []

// 单独看某一页的段落长度：node tools-site/rhythm.mjs --dump stdlib/046_text.md
if (process.argv[2] === '--dump') {
  const f = process.argv[3]
  for (const p of paragraphs(readFileSync(join(siteRoot, f), 'utf8'))) {
    console.log(String([...p].length).padStart(4), p)
  }
  process.exit(0)
}
for (const f of walk(siteRoot)) {
  const rel = relative(siteRoot, f).replace(/\\/g, '/')
  const s = stats(readFileSync(f, 'utf8'))
  // 体裁不同，节奏标准不同：教程要呼吸段，参考页允许短段＋表格（对照 MDN 参考与 Zig 语言参考的实测）。
  // tools/060_errors.md 是"编译器原话对照表"，正文只有几句，主体就是一张查询表，按参考页算。
  const 教程 = /^learn\/|^index\.md|^tools\/059_cli\.md/.test(rel)
  const problems = []
  if (s.中位 > (教程 ? 55 : 75)) problems.push(`段落中位 ${s.中位} 字，偏长`)
  if (s.短段比 < (教程 ? 20 : 12)) problems.push(`短段只占 ${s.短段比}%，要 ${教程 ? '25%～35%' : '≥12%'}`)
  if (s.一句成段 < 1) problems.push('一处一句话独占一段都没有，全篇一个长度')
  if (s.长段 > (教程 ? 2 : 0)) problems.push(`超过 140 字的段落有 ${s.长段} 处，拆掉`)
  if (s.表格行 > 24 && 教程) problems.push(`表格 ${s.表格行} 行，教程页每页 ≤1 张`)
  if (s.提示块 > 2) problems.push(`提示块 ${s.提示块} 个，每页 ≤2`)
  // `site/README.md` 是给在仓库里翻目录的人看的 readme，VitePress 用 `srcExclude` 把它排除在发布之外，
  // 所以"站上"这一类仓库内部的词在它那里不算违规；段落长短那几项照旧统计。
  if (s.黑名单.length && rel !== 'README.md') problems.push(`套话：${s.黑名单.join('、')}`)
  if (s.我们 > 3) problems.push(`"我们" 出现 ${s.我们} 次`)
  rows.push({ 页面: rel, ...s, 问题: problems })
}

let worst = 0
for (const r of rows) {
  if (!r.问题.length) continue
  worst++
  console.log(`\n✗ ${r.页面}  段${r.段数} 中位${r.中位} 短${r.短段比}% 长${r.长段} 短句${r.一句成段} 表${r.表格行} 提示${r.提示块}`)
  for (const p of r.问题) console.log(`    - ${p}`)
}
console.log(`\n共 ${rows.length} 页，${worst} 页不达标。`)
process.exitCode = worst === 0 ? 0 : 1
