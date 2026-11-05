// extension.ts 做两件事：把 `.nf` 文件交给 `nf lsp`（报错、补全、悬停），以及把 `nf dap`
// 注册成调试适配器。
//
// 语法高亮、注释符号与括号配对不经过这段代码：它们在 package.json 与
// language-configuration.json 里声明，由 VS Code 自己做。
//
// 用哪一份 `nf` 由设置里的 `nf.path` 说：扩展里不带编译器，本机更新了什么版本就用什么版本，
// 不会出现屏幕上两份编译器各说一套。那一项留空时按 PATH 里的 `nf` 走。
import * as fs from 'node:fs';
import * as vscode from 'vscode';
import { LanguageClient, TransportKind } from 'vscode-languageclient/node';
import type { ServerOptions } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

// nfWhere 交出两样：用哪一条命令起 `nf`，以及起不来的时候要怎么把话说完。
// 设置里填了路径就用那一条；填了却指不到一个文件，那是最常见的一种错，
// 要报的是"去哪里填"，不是操作系统那句 ENOENT。
//
// en: nfWhere returns two things: the command used to start `nf`, and how to finish the sentence
// if it will not start. A path in the setting wins; a path that points at no file is the most
// common mistake, and what belongs on screen is where to fill it in, not the operating system's
// ENOENT.
function nfWhere(): { command: string; howTo: string } {
  const configured = vscode.workspace.getConfiguration('nf').get<string>('path')?.trim() ?? '';
  if (configured) {
    if (!fs.existsSync(configured)) {
      return {
        command: configured,
        howTo: `设置里的 nf.path 指不到一个文件（${configured}）。改成这台机器上 nf.exe 的完整路径，编译器在仓库的 compiler/ 下用 go build 现编。`,
      };
    }
    return { command: configured, howTo: '改设置里的 nf.path，指到本机那一份 nf.exe。' };
  }
  return {
    command: 'nf',
    howTo: '扩展不带编译器：在设置里填 nf.path（本机 nf.exe 的完整路径），或者把 nf.exe 所在的目录加进 PATH。',
  };
}

// SETTINGS_ITEM 是错误提示上那一个按钮要去的地方：点了直接落在 nf.path 这一项上。
//
// en: the button on the error message drops straight onto the nf.path item.
const SETTINGS_ITEM = 'nf.path';

export function activate(context: vscode.ExtensionContext) {
  const { command, howTo } = nfWhere();
  const server: ServerOptions = {
    run: { command, args: ['lsp'], transport: TransportKind.stdio },
    debug: { command, args: ['lsp'], transport: TransportKind.stdio },
  };
  client = new LanguageClient('nf', 'nf 的语言服务器', server, {
    documentSelector: [{ scheme: 'file', language: 'nf' }],
  });

  client.start().then(undefined, (err: unknown) => {
    void reportMissingCompiler(err, command, howTo);
  });

  context.subscriptions.push({
    dispose: () => {
      void client?.stop();
    },
  });

  // 调试：VS Code 自己讲 DAP 那套协议，扩展要做的只是把 `nf dap` 起起来当适配器。
  context.subscriptions.push(
    vscode.debug.registerDebugAdapterDescriptorFactory('nf', {
      createDebugAdapterDescriptor() {
        const run = nfWhere();
        if (!fs.existsSync(run.command) && run.command !== 'nf') {
          void reportMissingCompiler(new Error('找不到那一个文件'), run.command, run.howTo);
        }
        return new vscode.DebugAdapterExecutable(run.command, ['dap']);
      },
    }),
    // 没有 launch.json 直接按 F5 时 program 是空的：用当前打开的那份文件填上。
    vscode.debug.registerDebugConfigurationProvider('nf', {
      resolveDebugConfiguration(_folder, config) {
        if (!config.program) {
          const editor = vscode.window.activeTextEditor;
          config.program = editor ? editor.document.fileName : '${file}';
        }
        return config;
      },
    }),
  );
}

// reportMissingCompiler 把起不来的那一条与"去哪里填"一起说一遍，并给一个直接跳到那一项的按钮。
//
// en: reportMissingCompiler says what failed together with where to fill the path in, and offers a
// button that lands on that very setting.
async function reportMissingCompiler(err: unknown, command: string, howTo: string) {
  const picked = await vscode.window.showErrorMessage(
    `nf 起不来（${String(err)}）。用的是 ${command}。${howTo}`,
    '打开 nf.path',
  );
  if (picked) {
    await vscode.commands.executeCommand('workbench.action.openSettings', SETTINGS_ITEM);
  }
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
