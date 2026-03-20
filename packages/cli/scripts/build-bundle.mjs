/**
 * esbuild 打包脚本 - 将 CLI 打包为单文件 bundle
 */
import * as esbuild from 'esbuild';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

await esbuild.build({
  entryPoints: [path.resolve(__dirname, '../src/index.ts')],
  bundle: true,
  platform: 'node',
  target: 'node18',
  format: 'cjs',
  outfile: path.resolve(__dirname, '../dist/codeflow.cjs'),
  minify: true,
  sourcemap: false,
  banner: {
    js: '#!/usr/bin/env node',
  },
  external: [
    // Native modules that can't be bundled
    'better-sqlite3',
  ],
  define: {
    'import.meta.url': 'undefined',
  },
});

console.log('✅ Bundle created: dist/codeflow.cjs');
