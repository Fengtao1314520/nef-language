# Nef

![Version](https://img.shields.io/badge/version-v0.4.1-blue)
[![License](https://img.shields.io/badge/license-Apache--2.0%20with%20Nef%20Output%20Exception-green)](LICENSE)
[![Docs](https://img.shields.io/badge/docs-CC%20BY%204.0-yellow)](nef-licensing/LICENSE-DOCS.md)
[![Platform](https://img.shields.io/badge/platform-Windows%20x64-lightgrey)](#requirements)
[![Built with Qoder](https://img.shields.io/badge/built%20with-Qoder-6c5ce7)](#acknowledgments)

中文说明见 [README.zh-CN.md](README.zh-CN.md).

Nef is a statically typed language with mandatory type annotations and no
garbage collection. Every value has exactly one owner at any moment: passing a
value to a function or storing it into a container hands ownership over - a
parameter written `ref` borrows it for the call instead - and the compiler frees each block at compile time, at the end of the scope of its
last owner. The compiler is written in Go and emits x86-64 machine code
directly, producing a standalone `.exe`.

Compiled programs ask the operating system for only a handful of capabilities
(standard output, memory, process exit, and the system libraries for sockets
and HTTP). No runtime, no installed components, no C runtime linkage.

## Contents

- [Highlights](#highlights)
- [Requirements](#requirements)
- [Build](#build)
- [A first program](#a-first-program)
- [Command line](#command-line)
- [Tests](#tests)
- [Repository layout](#repository-layout)
- [Contributing](#contributing)
- [License](#license)
- [Acknowledgments](#acknowledgments)

## Highlights

- Ownership moves on assignment and on argument passing; write `ref` before a
  parameter to borrow it instead. The destructor order is the reverse of
  declaration, decided at compile time.
- The compiler runs a program straight from memory (`nf file.nf`) or builds a
  standalone executable (`nf build`) - same checks, same machine code.
- A small standard library of fourteen groups, covering text, containers,
  files, time, math, JSON, regular expressions, networking (TCP and HTTP) and
  processes.
- Packages ship as source (`.nfpack` containers), compiled again by each
  consuming project.
- A VS Code extension with syntax highlighting, diagnostics, completion,
  hover docs and F5 debugging, built on `nf lsp` and `nf dap`.
- `webtest/` - a browser automation library written in Nef itself, the first
  external program built with this toolchain.

## Requirements

- Windows x64. This is the only supported target for the compiler and for
  built programs; the generated PE imports `kernel32`, `ws2_32` and
  `winhttp`, and there is no platform abstraction layer yet.
- Go 1.27 or newer to build the compiler (`go version go1.27.1 windows/amd64`
  is what this repository is tested against).
- Node.js and npm to work on the website and the VS Code extension.

## Build

```text
cd compiler
go build -trimpath -o ../nf.exe ./cmd/nf
```

`nf.exe` lands at the repository root; that is the binary you run the samples
with. `./nf.exe version` prints its version, and a build with no version
injected says `nf dev`. To stamp one, add
`-ldflags "-X main.version=vX.Y.Z"` to the command above. The `nf.exe` and the
VS Code extension package for each release are attached to that release's
GitHub Releases page. Which digit of the version moves, and when, is
explained in [版本与兼容](site/about/066_versions.md) (site pages are Chinese-only
for now); what changed in each version is listed in [CHANGELOG.md](CHANGELOG.md).

Run a sample directly, or build it into an executable:

```text
./nf.exe samples/shapes.nf
./nf.exe build samples/stock.nf -o stock.exe
./stock.exe
```

## A first program

```nf
let name:string = "Nef"
let xs:list<int> = [1, 2, 3]
let total:int = 0
for (x:int in xs) {
    total = total + x
}
console.print("hello, " + name + "; sum = " + number.to_string(total))
```

Save it as `hello.nf` and run `./nf.exe hello.nf`. Type annotations are part
of the declaration; `total = total + x` rebinds the owner of an `int`, while
`let one:string = xs[0]` would move the element out of the list and leave that
slot empty.

## Command line

| Command | What it does |
| --- | --- |
| `nf file.nf [args...]` | Compile in memory and run; no intermediate files |
| `nf build file.nf [-o out.exe]` | Emit a standalone executable |
| `nf pack facade.nf [-o pkg.nfpack]` | Pack a package's sources into a `.nfpack` container for other projects |
| `nf check file.nf` | Lex, parse and type-check; diagnostics carry file, line and column |
| `nf tokens file.nf` | Print the token stream (`kind:text`, one per line) - the site highlighter is checked against it |
| `nf ir file.nf` | Print the lowered intermediate code, without emitting machine code |
| `nf lsp` | Language server over stdio; same checking rules as `nf check` |
| `nf dap` | Debug adapter for VS Code: breakpoints, stepping, call stack, variables |
| `nf version` | Print the compiler version; exit code is always 0 |

## Tests

```text
cd compiler
go test ./...
```

The suite checks the language rules, the lowered code and whole programs.
Every file in `samples/` has an end-to-end test; several build the program into
a real executable, run it as a child process and compare the output line by
line. Every sentence the compiler says to a user is collected in
`compiler/testdata/报错文字.txt` and compared against the source, so rewording a
message without regenerating that list fails the suite.

Every `.go` file under `compiler/` opens with the Apache-2.0 header from
`nef-licensing/templates/FILE_HEADER.txt`. Comments describe the code: they do
not cite section numbers of the specification, numbered rulings, or document
names that are not in the published tree.

The website has its own checks (`npm run verify` in `site/`): every `nf` code
block must compile, every "wrong way" block must be rejected with the exact
compiler sentence, and every claimed output is produced by actually running the
example.

## Repository layout

| Path | Contents |
| --- | --- |
| `compiler/` | The Go source of the whole toolchain (single module, no third-party dependencies) |
| `samples/` | Seven example programs; each one doubles as an end-to-end test |
| `webtest/` | Browser automation library and test runner written in Nef |
| `site/` | The documentation website (VitePress) and its seven checks |
| `vscode/` | The VS Code extension |
| `.github/workflows/` | The GitHub Pages workflow that builds and publishes the documentation site |
| `nef-licensing/` | The remaining license texts: CLA, trademark policy, contribution flow, docs license, Apache original, header and exception templates |
| `CHANGELOG.md` | What changed in each version |

## Contributing

Read [nef-licensing/CONTRIBUTING.md](nef-licensing/CONTRIBUTING.md) and the
contributor license agreement next to it. Two conventions shape changes here:

- Compiler error sentences are part of the interface: they are recorded in
  tests and in the documentation verbatim, never from memory.
- When an automated check fails, the expectation is not adjusted to make it
  pass; only a deliberate language decision may change an expectation, and the
  rule behind it is written into the specification on the documentation site
  first, so the change is reviewed as a language change rather than as a test
  fix.

## License

- Code (compiler, toolchain, runtime, standard library, extension): Apache
  License 2.0 with the **Nef Output Exception** - machine code generated by
  `nf` is not subject to any copyleft claim. The License text is
  [LICENSE](LICENSE), the exception is [LICENSE-EXCEPTION](LICENSE-EXCEPTION),
  and third-party attributions are in [NOTICE](NOTICE); all three ship inside
  the VS Code extension package.
- Documentation, specification and website prose: [CC BY 4.0](site/LICENSE).
  The site's own scripts under `site/tools-site/` and `site/.vitepress/` are
  code and follow the line above.
- The name "Nef" is governed by the [trademark policy](nef-licensing/TRADEMARK_POLICY.md).
- The seven programs under `samples/` and the code printed in this README or on
  the documentation site are dedicated to be free of restriction: use them in
  your own programs under any terms you like, no attribution needed. The
  dedication is the last entry of the license summary in
  [NOTICE](NOTICE).

## Acknowledgments

The compiler, the standard library, the website, the VS Code extension and
this documentation were developed inside [Qoder](https://qoder.com), an agentic
coding IDE - the language design, the architecture and every merge decision
remained with the author.
