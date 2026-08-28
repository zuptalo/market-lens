/// <reference types="vitest" />
import { defineConfig } from 'vitest/config';
import path from 'node:path';

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify('dev') },
  resolve: { alias: { '@': path.resolve(__dirname, './src') } },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      include: ['src/utils/**/*.ts'],
    },
  },
});
