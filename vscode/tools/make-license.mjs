// make-license.mjs 在打包前把仓库根的 LICENSE、LICENSE-EXCEPTION 与 NOTICE 拷进扩展，打完再删掉。
//
// 带上它们的理由：`vsce package` 找不到 LICENSE 会警告，扩展详情页的许可栏也会空着。
// NOTICE 是另一条理由：vsix 里嵌着一份 `nf.exe`，那是 Go 静态链接出来的产物，
// 根 NOTICE 里的第三方署名要随这份分发一起给出去。
// 用完就删的理由：许可全文的唯一出处是仓库根那一份；扩展目录里留副本，
// 时间一久就难以辨别哪一份是最新的。
// 用 `--clean` 删。
import { copyFileSync, existsSync, rmSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const extRoot = resolve(here, '..')
const repoRoot = resolve(extRoot, '..')

const pairs = [
  { source: join(repoRoot, 'LICENSE'), out: join(extRoot, 'LICENSE') },
  { source: join(repoRoot, 'LICENSE-EXCEPTION'), out: join(extRoot, 'LICENSE-EXCEPTION') },
  { source: join(repoRoot, 'NOTICE'), out: join(extRoot, 'NOTICE') },
]

if (process.argv.includes('--clean')) {
  for (const { out } of pairs) {
    rmSync(out, { force: true })
  }
  console.log('扩展里那三份许可、例外与署名副本已删掉，全文仍只有仓库根那三份')
  process.exit(0)
}

for (const { source, out } of pairs) {
  if (!existsSync(source)) {
    console.error(`找不到 ${source}：许可全文应该放在仓库根`)
    process.exit(1)
  }
  copyFileSync(source, out)
  console.log(`已拷进扩展：${out} ${statSync(out).size} 字节，来自 ${source}`)
}
