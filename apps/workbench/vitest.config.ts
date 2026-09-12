import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

const workbenchRoot = path.dirname(fileURLToPath(import.meta.url));

// @vitejs/plugin-react 解析到 workbench 的 vite 6 类型，vitest/config 的 PluginOption 来自根 vite 7，
// 两份 vite 类型副本互不兼容（纯类型层问题，运行时已由 FU 全部通过证明无碍），在此显式桥接。
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const reactPlugin = react() as any;

export default defineConfig({
  root: workbenchRoot,
  plugins: [reactPlugin],
  resolve: {
    alias: {
      '@': workbenchRoot,
    },
  },
  test: {
    globals: true,
    environment: 'node',
    environmentMatchGlobs: [['src/shell/pages/**/*.test.{ts,tsx}', 'jsdom']],
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: ['**/node_modules/**', '**/dist/**'],
    testTimeout: 30000,
    hookTimeout: 30000,
  },
});
