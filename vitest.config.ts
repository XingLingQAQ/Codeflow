import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['packages/**/src/**/*.test.ts', 'packages/**/src/**/__tests__/*.ts'],
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      'packages/cli/**',
      'packages/gui/**',
      'packages/shared/**',
      'packages/ui-components/**',
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      include: ['packages/**/src/**/*.ts'],
      exclude: [
        '**/node_modules/**',
        '**/dist/**',
        '**/*.test.ts',
        '**/__tests__/**',
        '**/index.ts',
        'packages/cli/**',
        'packages/gui/**',
        'packages/shared/**',
        'packages/ui-components/**',
      ],
    },
    testTimeout: 30000,
    hookTimeout: 30000,
  },
});
