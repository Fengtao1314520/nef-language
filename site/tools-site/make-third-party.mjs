// make-third-party.mjs 生成 public/third-party-licenses.txt：站上发布物里那些第三方依赖各自的许可声明。
//
// 这份文件的理由：`npm run build` 把 VitePress、Vue、shiki 这些包的运行时代码打进 `dist`，
// MIT 与 BSD 那几种都要求分发时把许可文本与版权行一起给出去。仓库根的 NOTICE 只管 `nf.exe`
// 那一侧（Go 静态链接），网站这一侧的署名跟着站点走，所以落成这个文件。
//
// 跑法：npm run third-party（依赖有变动之后重跑；不在 `npm run verify` 那七项里）
// 输出：public/third-party-licenses.txt，vitepress 把 public/ 原样拷进产物，站上访问路径是 /third-party-licenses.txt
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const siteRoot = resolve(here, '..')
const modulesRoot = join(siteRoot, 'node_modules')
const out = join(siteRoot, 'public', 'third-party-licenses.txt')

// 一个包目录：读 package.json 拿名字、版本、license 字段，再找许可文本文件与其中的版权行
function readPackage(dir) {
  let meta
  try {
    meta = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
  } catch {
    return null
  }
  if (!meta.name || !meta.version) return null
  let license = ''
  if (typeof meta.license === 'string') license = meta.license
  else if (meta.license && meta.license.type) license = meta.license.type
  else if (Array.isArray(meta.licenses) && meta.licenses.length) {
    license = meta.licenses.map((l) => l.type || '').filter(Boolean).join(' OR ')
  }

  let licenseFile = ''
  for (const name of ['LICENSE', 'LICENSE.md', 'LICENSE.txt', 'License', 'License.txt', 'LICENSE-MIT', 'Copying']) {
    const p = join(dir, name)
    if (existsSync(p) && statSync(p).isFile()) {
      licenseFile = name
      break
    }
  }
  let copyright = ''
  if (licenseFile) {
    const text = readFileSync(join(dir, licenseFile), 'utf8')
    const line = text.split(/\r?\n/).find((l) => /copyright/i.test(l) && l.trim().length > 10)
    if (line) copyright = line.trim().replace(/\s+/g, ' ')
  }
  // 没有 license 字段、文本写着 "SEE LICENSE IN ..." 的那一类，原样把那句话带出来，供人工核对
  if (!license && licenseFile) license = `见 ${licenseFile}`
  if (!license) license = '未标出'
  return { name: meta.name, version: meta.version, license, licenseFile, copyright }
}

// 走 node_modules，@scope 那一种要再下一层；包里的嵌套 node_modules 不算（站点产物用的是顶层提升后的那一份）
function collect() {
  const found = []
  for (const entry of readdirSync(modulesRoot)) {
    if (entry.startsWith('.')) continue
    const p = join(modulesRoot, entry)
    if (!statSync(p).isDirectory()) continue
    if (entry.startsWith('@')) {
      for (const sub of readdirSync(p)) {
        const info = readPackage(join(p, sub))
        if (info) found.push(info)
      }
      continue
    }
    const info = readPackage(p)
    if (info) found.push(info)
  }
  return found.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
}

const packages = collect()
const byLicense = {}
for (const p of packages) byLicense[p.license] = (byLicense[p.license] || 0) + 1

const lines = []
lines.push('文档网站发布物里的第三方组件与各自的许可')
lines.push('')
lines.push('这一份由 site/tools-site/make-third-party.mjs 生成，命令是 `npm run third-party`。')
lines.push(`生成时间 ${new Date().toISOString().slice(0, 10)}，来源是当时装在 node_modules 里的 ${packages.length} 个包。`)
lines.push('站点构建（`npm run build`）会把其中进入运行时的部分打进 dist，随站点一起分发；这些组件的许可文本与版权行按要求在这里列出。')
lines.push('')
lines.push('按许可类型汇总：')
for (const [k, v] of Object.entries(byLicense).sort((a, b) => b[1] - a[1] || (a[0] < b[0] ? -1 : 1))) {
  lines.push(`  ${k} — ${v} 个包`)
}
lines.push('')
lines.push('逐个列表：')
lines.push('')
for (const p of packages) {
  lines.push(`${p.name}@${p.version}`)
  lines.push(`  许可：${p.license}`)
  if (p.licenseFile) lines.push(`  许可文本：node_modules 里的 ${p.licenseFile}`)
  if (p.copyright) lines.push(`  版权行：${p.copyright}`)
  lines.push('')
}

lines.push('随站点一起分发的字体')
lines.push('')
lines.push('  Inter —— VitePress 默认主题自带，构建时作为 woff2 拷进 dist/assets，被每个页面引用')
lines.push('    许可：SIL Open Font License 1.1')
lines.push('    版权行：Copyright (c) 2016 The Inter Project Authors (https://github.com/rsms/inter)')
lines.push('    许可文本：仓库内的 public/inter-font-license.txt，发布之后是站点根上的 /inter-font-license.txt')
lines.push('')

writeFileSync(out, lines.join('\n'), 'utf8')
console.log(`写了 ${out}`)
console.log(`${packages.length} 个包，许可类型 ${Object.keys(byLicense).length} 种`)
