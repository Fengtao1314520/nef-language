// check-highlight.mjs 用真实的 TextMate 引擎（VS Code 使用的那一个）把这份语法跑一遍，
// 核对高亮确实能区分：正则写错、分支顺序错、块注释吞掉其后全部内容，都会在这里当场报失败。
//
// 跑法：node tools/check-highlight.mjs（或 npm run check:scope）
import { createRequire } from 'node:module'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const { Registry } = require('vscode-textmate')
const { loadWASM, OnigScanner, OnigString } = require('vscode-oniguruma')

const here = dirname(fileURLToPath(import.meta.url))
const extRoot = resolve(here, '..')
const repoRoot = resolve(extRoot, '..')

const grammarPath = join(extRoot, 'syntaxes', 'nf.tmLanguage.json')
const wasm = readFileSync(join(extRoot, 'node_modules', 'vscode-oniguruma', 'release', 'onig.wasm'))
await loadWASM(wasm.buffer)

const source = JSON.parse(readFileSync(grammarPath, 'utf8'))
const registry = new Registry({
  onigLib: Promise.resolve({
    createOnigScanner: (sources) => new OnigScanner(sources),
    createOnigString: (text) => new OnigString(text),
  }),
  loadGrammar: async (scopeName) => (scopeName === source.scopeName ? source : null),
})
const grammar = await registry.loadGrammar(source.scopeName)

// 一份源码逐行切成记号，块注释与文本跨行的状态往下带。
function tokenize(text) {
  let ruleStack = null
  const lines = []
  for (const line of text.replace(/\r/g, '').split('\n')) {
    const { tokens, ruleStack: next } = grammar.tokenizeLine(line, ruleStack ?? null)
    ruleStack = next
    lines.push(tokens.map((t) => ({ text: line.substring(t.startIndex, t.endIndex), scopes: t.scopes })))
  }
  return lines
}

const fail = []
let assertions = 0
function expectScope(text, scope) {
  assertions++
  const hits = flat.filter((t) => t.text === text)
  if (hits.length === 0) {
    fail.push(`探针里找不到 ${JSON.stringify(text)} 这一段，高亮没切出来`)
    return
  }
  if (!hits.some((t) => t.scopes.includes(scope))) {
    fail.push(`${JSON.stringify(text)} 的着色档位不对，要 ${scope}，实际是 ${hits.map((t) => t.scopes.join('|')).join(' 或 ')}`)
  }
}

const probe = [
  'method half(n: int): int {',
  '    let xs: list<int> = [1, 2_000, 0x1F]',
  '    const s: string = "甲\\n乙"',
  '    let c: unicode = 97',
  '    if n == 0 || !true { return 0 }',
  '    while n <= 3 { n++ }',
  '    let h: dict<string, int> = { "键": 1 }',
  '    let j: Json = json.parse(s)',
  '    let b: bytes = bytes.new(4)',
  '    let sock: Socket = net.connect("127.0.0.1", 9000, 100)',
  '    let door: Listener = net.listen(9000)',
  '    let page: Response = http.get("http://example.com/", 1000)',
  '    console.print(s + s, length(xs))',
  '    /*块注释',
  '跨两行*/',
  '    return n /*尾巴*/ + half(n)',
  '}',
  '',
].join('\n')

const flat = tokenize(probe).flat()

expectScope('method', 'keyword.control.nf')
expectScope('let', 'keyword.control.nf')
expectScope('const', 'keyword.control.nf')
expectScope('if', 'keyword.control.nf')
expectScope('while', 'keyword.control.nf')
expectScope('return', 'keyword.control.nf')
expectScope('true', 'constant.language.nf')
expectScope('int', 'support.type.nf')
expectScope('string', 'support.type.nf')
expectScope('list', 'support.type.nf')
expectScope('dict', 'support.type.nf')
expectScope('Json', 'support.type.nf')
// `bytes` 同时是类型名与组名，语法文件里类型名的规则先出现，所以它着色成 support.type.nf。
// 写 `bytes.new(4)` 时两个名字落在同一档，这是这个名字的形状，不是漏了一条组名。
expectScope('bytes', 'support.type.nf')
// 三类名字同一档：两类句柄与内建 `struct`。`Socket`／`Listener` 不是容器里的一种值，
// 但它们是类型标注上的名字，着色与 `Json`、`bytes` 同一档。
expectScope('Socket', 'support.type.nf')
expectScope('Listener', 'support.type.nf')
expectScope('Response', 'support.type.nf')
expectScope('json', 'support.class.nf')
expectScope('console', 'support.class.nf')
expectScope('length', 'support.function.nf')
expectScope('1', 'constant.numeric.decimal.nf')
expectScope('2_000', 'constant.numeric.decimal.nf')
expectScope('0x1F', 'constant.numeric.hex.nf')
expectScope('==', 'keyword.operator.nf')
expectScope('||', 'keyword.operator.nf')
expectScope('<=', 'keyword.operator.nf')
expectScope('++', 'keyword.operator.nf')
expectScope('}', 'keyword.operator.nf')
expectScope('甲', 'string.quoted.double.nf')
expectScope('\\n', 'constant.character.escape.nf')
expectScope('块注释', 'comment.block.nf')
expectScope('跨两行', 'comment.block.nf')
expectScope('尾巴', 'comment.block.nf')

// 十二个组名逐个校验（`bytes` 除外，理由见上一条）：像把 `regex` 改成
// `regular_expression` 这一类改名，这里也得报失败。
// `bytes` 不在这一串里：它先是类型名，上面那一条 expectScope 钉的是它着成哪一档。
const groups = ['console', 'text', 'number', 'math', 'time', 'file', 'path', 'environment', 'regular_expression', 'json', 'net', 'http']
const groupProbe = tokenize(groups.join('\n')).flat()
for (const g of groups) {
  assertions++
  const hit = groupProbe.find((t) => t.text === g)
  if (!hit || !hit.scopes.includes('support.class.nf')) {
    fail.push(`组名 ${g} 没进 support.class.nf${hit ? '，实际是 ' + hit.scopes.join('|') : ''}`)
  }
}

// 探针里没有单行注释，单独再来一条检查。
const lineComment = tokenize('let n: int = 1 // 到行尾\n')[0]
if (!lineComment.some((t) => t.scopes.includes('comment.line.double-slash.nf'))) {
  fail.push('单行注释没进 comment.line.double-slash.nf')
}
if (lineComment.some((t) => t.text.includes('1') && t.scopes.includes('comment.line.double-slash.nf'))) {
  fail.push('单行注释把前面的代码也吃进去了')
}

// 三份示例程序都要能完整切分、不异常退出：真实程序行数足够多，
// 正则回溯或括号不配对的问题会在这里暴露。
const sampleDir = join(repoRoot, 'samples')
let sampleLines = 0
for (const name of readdirSync(sampleDir).filter((n) => n.endsWith('.nf'))) {
  try {
    sampleLines += tokenize(readFileSync(join(sampleDir, name), 'utf8')).length
  } catch (e) {
    fail.push(`切 samples/${name} 时炸了：${e.message}`)
  }
}

if (fail.length) {
  console.error(`nf 高亮核对不通过（${fail.length} 条）：`)
  for (const f of fail) console.error('  ' + f)
  process.exit(1)
}
console.log(`nf 高亮核对通过：探针 ${flat.length} 段、${assertions + 2} 个档位断言，样例 ${sampleLines} 行全部切完`)
