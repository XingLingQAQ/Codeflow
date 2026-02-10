/**
 * Shadow CLI 命令 - MS-140
 *
 * 提供 shadow init / project / sync 三个子命令，
 * 分别用于初始化影子目录、批量投影和文件变更监听。
 */

import { Command } from 'commander';
import {
  ShadowScaffold,
  BatchProjector,
  IntentProjector,
  DynamicSync,
} from '@codeflow/core';

/**
 * 注册 shadow 子命令到 Commander program
 */
export function registerShadowCommand(program: Command): void {
  const shadow = program
    .command('shadow')
    .description('影子系统操作：初始化、投影、同步');

  shadow
    .command('init')
    .description('初始化 .codeflow/ 影子目录结构')
    .argument('[path]', '项目路径', process.cwd())
    .action(async (projectPath: string) => {
      try {
        const scaffold = new ShadowScaffold();
        await scaffold.initialize(projectPath);
        console.log(`✅ Shadow system initialized at ${projectPath}`);
      } catch (err) {
        console.error('❌ Failed to initialize shadow system:', (err as Error).message);
        process.exit(1);
      }
    });

  shadow
    .command('project')
    .description('批量投影目录中的源文件为意图文档')
    .argument('[dir]', '要投影的目录', 'src')
    .option('--root <path>', '项目根目录', process.cwd())
    .action(async (dir: string, opts: { root: string }) => {
      try {
        const projector = new IntentProjector();
        const batch = new BatchProjector(projector, (msg) => console.log(msg));
        const result = await batch.projectDirectory(dir, opts.root);

        console.log(`\n✅ Intent projection completed: ${result.succeeded} succeeded, ${result.failed} failed`);
      } catch (err) {
        console.error('❌ Failed to project directory:', (err as Error).message);
        process.exit(1);
      }
    });

  shadow
    .command('sync')
    .description('监听文件变更，自动更新意图文档')
    .option('--root <path>', '项目根目录', process.cwd())
    .option('--debounce <ms>', '防抖延迟（毫秒）', '500')
    .action(async (opts: { root: string; debounce: string }) => {
      try {
        const projector = new IntentProjector();
        const batch = new BatchProjector(projector, (msg) => console.log(msg));
        const sync = new DynamicSync(batch, {
          projectRoot: opts.root,
          debounceMs: parseInt(opts.debounce, 10),
        }, (event) => {
          switch (event.type) {
            case 'start':
              console.log('🔄 Syncing...');
              break;
            case 'file_synced':
              console.log(`  ✅ ${event.filePath}`);
              break;
            case 'file_failed':
              console.error(`  ❌ ${event.filePath}: ${event.error}`);
              break;
            case 'complete':
              console.log(`✅ Sync complete (${event.completed}/${event.total})`);
              break;
          }
        });

        sync.start();
        console.log(`👀 Watching for file changes in ${opts.root}...`);
        console.log('   Press Ctrl+C to stop.');

        // Keep process alive
        process.on('SIGINT', () => {
          sync.stop();
          console.log('\n🛑 Stopped watching.');
          process.exit(0);
        });
      } catch (err) {
        console.error('❌ Failed to start sync:', (err as Error).message);
        process.exit(1);
      }
    });
}
