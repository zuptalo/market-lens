import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
import path from 'node:path';

export default defineConfig({
  plugins: [vue()],
  define: { __APP_VERSION__: JSON.stringify('test') },
  resolve: { alias: { '@': path.resolve(__dirname, './src') } },
  // A secure origin is required so __Host- prefixed cookies behave as they do in a browser.
  test: {
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'https://localhost/' } },
    include: ['src/**/*.test.ts'],
    setupFiles: ['./src/test-setup.ts'],
  },
});
