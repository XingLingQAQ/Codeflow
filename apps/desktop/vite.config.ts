import path from 'path';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const host = process.env.TAURI_DEV_HOST;

export default defineConfig(({ mode }) => {
    const env = loadEnv(mode, '.', '');
    return {
      base: '/',
      clearScreen: false,
      server: {
        port: 3000,
        strictPort: true,
        host: host || '0.0.0.0',
        hmr: host
          ? {
              protocol: 'ws',
              host,
              port: 3001,
            }
          : undefined,
        watch: {
          ignored: ['**/src-tauri/**'],
        },
      },
      build: {
        outDir: 'dist',
        target: ['es2021', 'chrome100', 'safari13'],
        minify: !process.env.TAURI_DEBUG ? 'esbuild' : false,
        sourcemap: !!process.env.TAURI_DEBUG,
      },
      envPrefix: ['VITE_', 'TAURI_'],
      plugins: [react(), tailwindcss()],
      define: {
        'process.env.API_KEY': JSON.stringify(env.GEMINI_API_KEY),
        'process.env.GEMINI_API_KEY': JSON.stringify(env.GEMINI_API_KEY)
      },
      resolve: {
        alias: {
          '@': path.resolve(__dirname, '.'),
        }
      }
    };
});
