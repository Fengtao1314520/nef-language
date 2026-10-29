// nef 的高亮：记号规则对齐 SPEC §7（关键字、保留名、字面量、两种注释、运算符）。
//
// 这个文件与 compiler/internal/lexer 认同一套记号；
// site/tools-site/check-highlight.mjs 把两边的记号流逐条对齐，改了一边另一边就报错。

const KEYWORDS = new Set(
  ('let const method struct class interface enum type import export shared base this new return ' +
    'as is throw try catch if else while for in until switch default break continue true false null').split(/\s+/)
)

// 这一张名单与 compiler/internal/lexer/lexer.go 的 `Reserved` 一一对齐（33 个）：
// 少一个就有名字在这两边落进不同的档位，check-site.mjs 的高亮对照那一项当场报出来。
const RESERVED = new Set(
  ('boolean int float byte unicode string object list dict hashset Calendar Json bytes ' +
    'Socket Listener Process Response length exit ' +
    'console text number math time file path environment regular_expression json net http process args').split(/\s+/)
)

const OPS = [
  '==', '!=', '<=', '>=', '&&', '||', '++', '--', '<<', '>>',
  '+', '-', '*', '/', '%', '!', '=', '<', '>', '&', '|', '^', '~',
  '(', ')', '{', '}', '[', ']', ':', ',', ';', '.'
]

const isDigit = (c) => c >= '0' && c <= '9'
const isHex = (c) => isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
const isNameStart = (c) => c === '_' || c === '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
const isNamePart = (c) => isNameStart(c) || isDigit(c)

export function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// tokenize 交回 {kind, text}：kind 与 nf.exe tokens 打出的类型同名（nl/name/num/str/char/op），
// 只是这里额外把关键字、保留名、注释单列出来，上色要用。
export function tokenize(src) {
  const out = []
  const push = (kind, text) => out.push({ kind, text })
  let i = 0
  while (i < src.length) {
    const c = src[i]

    if (c === '\n') { push('nl', c); i++; continue }
    if (c === ' ' || c === '\t' || c === '\r') {
      let j = i
      while (j < src.length && (src[j] === ' ' || src[j] === '\t' || src[j] === '\r')) j++
      push('plain', src.slice(i, j)) // 行内空白自己不上色，但要留在输出里
      i = j
      continue
    }

    if (c === '/' && src[i + 1] === '/') {
      let j = i
      while (j < src.length && src[j] !== '\n') j++
      push('comment', src.slice(i, j))
      i = j
      continue
    }
    if (c === '/' && src[i + 1] === '*') {
      let j = i + 2
      while (j < src.length && !(src[j] === '*' && src[j + 1] === '/')) j++
      push('comment', src.slice(i, Math.min(j + 2, src.length)))
      i = j + 2
      continue
    }

    if (isNameStart(c)) {
      let j = i
      while (j < src.length && isNamePart(src[j])) j++
      const word = src.slice(i, j)
      push(KEYWORDS.has(word) ? 'keyword' : RESERVED.has(word) ? 'reserved' : 'name', word)
      i = j
      continue
    }

    if (isDigit(c)) {
      let j = i
      if (c === '0' && (src[i + 1] === 'x' || src[i + 1] === 'X')) {
        j = i + 2
        while (j < src.length && (isHex(src[j]) || src[j] === '_')) j++
      } else {
        while (j < src.length && (isDigit(src[j]) || src[j] === '_')) j++
        if (src[j] === '.' && isDigit(src[j + 1])) {
          j++
          while (j < src.length && (isDigit(src[j]) || src[j] === '_')) j++
        }
      }
      push('num', src.slice(i, j))
      i = j
      continue
    }

    if (c === '"' || c === "'") {
      let j = i + 1
      while (j < src.length && src[j] !== c && src[j] !== '\n') {
        if (src[j] === '\\') j++
        j++
      }
      push(c === '"' ? 'str' : 'char', src.slice(i, j + 1))
      i = j + 1
      continue
    }

    const op = OPS.find((o) => src.startsWith(o, i))
    if (op) { push('op', op); i += op.length; continue }

    push('plain', c)
    i++
  }
  return out
}

const CLS = {
  keyword: 'k', reserved: 'r', name: 'n', num: 'num', str: 's', char: 'c',
  comment: 'cm', op: 'o'
}

export function highlightNef(code) {
  let html = '<span class="nef-code">'
  for (const t of tokenize(code)) {
    const body = escapeHtml(t.text)
    if (t.kind === 'nl' || t.kind === 'plain') { html += body; continue }
    const c = CLS[t.kind]
    html += c ? `<span class="${c}">${body}</span>` : body
  }
  return html + '</span>'
}
