// make-icon.mjs 把站点那一份品牌图形渲染成 VS Code 需要的 PNG 图标。
//
// 图形的唯一出处是 `site/public/favicon.svg`：这份脚本不另画图形，也不留第二份源文件；
// 改那份 SVG 后重跑这条命令即自动同步（npm run package 会自己带上它）。
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { Resvg } = require('@resvg/resvg-js')

const here = dirname(fileURLToPath(import.meta.url))
const extRoot = resolve(here, '..')
const repoRoot = resolve(extRoot, '..')

const source = join(repoRoot, 'site', 'public', 'favicon.svg')
const out = join(extRoot, 'images', 'icon.png')

// VS Code 的最低要求是 128×128，取 256 对应侧栏与扩展页的最大显示档位。
const svg = readFileSync(source, 'utf8')
const png = new Resvg(svg, { fitTo: { mode: 'width', value: 256 } }).render().asPng()

if (png.length === 0) {
  console.error('切出来的图标是空的，检查 ' + source)
  process.exit(1)
}
mkdirSync(dirname(out), { recursive: true })
writeFileSync(out, png)

// SVG 的 viewBox 是 64 见方，渲染成 256 正好 4 倍；输出边长确为 256 才算正确。
const width = png.readUInt32BE(16)
const height = png.readUInt32BE(20)
if (width !== 256 || height !== 256) {
  console.error(`图标尺寸不对：想要 256×256，得到 ${width}×${height}`)
  process.exit(1)
}
console.log(`图标已切出：${out} ${width}×${height}，${png.length} 字节，来自 ${source}`)
