// make-server.mjs 在打包前当场编一份带版本号的编译器，放进扩展的 server/ 目录，打完再删掉。
//
// 为什么要现编而不是拷仓库根那一份：根目录的 `nf.exe` 是开发用的，`nf version` 打印 `nf dev`；
// 扩展的 `nf lsp` 与 `nf dap` 用的就是这一份，于是插件版本号与它自带的编译器版本号对不上。
// 现在用 vscode/package.json 里的 version 注入进去，插件 0.4.1 带的就是 `nf v0.4.1`。
//
// 为什么用完就删：这份产物只在打包过程中需要，留在目录里就没人知道哪一份是新的；
// 平时用的那一份仍然只有仓库根那一个。
// 用 `--clean` 删。
import { execFileSync } from 'node:child_process'
import { mkdirSync, readFileSync, rmSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const extRoot = resolve(here, '..')
const repoRoot = resolve(extRoot, '..')
const moduleDir = join(repoRoot, 'compiler')

const exeName = process.platform === 'win32' ? 'nf.exe' : 'nf'
const out = join(extRoot, 'server', exeName)

if (process.argv.includes('--clean')) {
  rmSync(join(extRoot, 'server'), { recursive: true, force: true })
  console.log('扩展里那份编译器已删掉，平时用的那一份仍然只有仓库根那个')
  process.exit(0)
}

const version = JSON.parse(readFileSync(join(extRoot, 'package.json'), 'utf8')).version
if (!version) {
  console.error('读不到 vscode/package.json 里的 version，注不进版本号')
  process.exit(1)
}

mkdirSync(dirname(out), { recursive: true })
// -trimpath 不嵌构建机的源码目录；版本号在这里注入，与扩展自己的版本号同一个来源。
execFileSync('go', ['build', '-trimpath', '-ldflags', `-X main.version=v${version}`, '-o', out, './cmd/nf'], {
  cwd: moduleDir,
  stdio: 'inherit',
})
const printed = execFileSync(out, ['version'], { encoding: 'utf8' }).trim()
console.log(`已编进扩展：${out} ${statSync(out).size} 字节，${exeName} version 打印 ${printed}`)
if (printed !== `nf v${version}`) {
  console.error(`注入的版本号与扩展版本号不一致：期望 nf v${version}`)
  process.exit(1)
}
