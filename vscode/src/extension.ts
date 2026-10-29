// extension.ts 做两件事：把 `.nf` 文件交给 `nf lsp`（报错、补全、悬停），以及把 `nf dap`
// 注册成调试适配器。
//
// 语法高亮、注释符号与括号配对不经过这段代码：它们在 package.json 与
// language-configuration.json 里声明，由 VS Code 自己做。要用的 `nf` 默认是扩展自带的那一份
// （打包时从仓库根拷进来，见 tools/make-server.mjs）；设置里填了 `nf.path` 就用填的那一份。
import * as vscode from 'vscode';
import { LanguageClient, TransportKind } from 'vscode-languageclient/node';
import type { ServerOptions } from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

function nfCommand(context: vscode.ExtensionContext): string {
  const configured = vscode.workspace.getConfiguration('nf').get<string>('path');
  if (configured && configured.trim()) {
    return configured.trim();
  }
  return context.asAbsolutePath('server/nf.exe');
}

export function activate(context: vscode.ExtensionContext) {
  const command = nfCommand(context);
  const server: ServerOptions = {
    run: { command, args: ['lsp'], transport: TransportKind.stdio },
    debug: { command, args: ['lsp'], transport: TransportKind.stdio },
  };
  client = new LanguageClient('nf', 'nf 的语言服务器', server, {
    documentSelector: [{ scheme: 'file', language: 'nf' }],
  });

  // 起不来最常见的是扩展里那一份 nf 没带上（打包时没跑 npm run server），或者设置里
  // nf.path 指错了地方：这句话要说清两条出路，不能只报一个 ENOENT。
  client.start().then(undefined, (err: unknown) => {
    vscode.window.showErrorMessage(
      `nf 的语言服务器起不来（${String(err)}）。用的是 ${command}：要么把 nf.path 改成本机上的 nf.exe，要么重装带编译器的扩展。`,
    );
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
        return new vscode.DebugAdapterExecutable(nfCommand(context), ['dap']);
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

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}
