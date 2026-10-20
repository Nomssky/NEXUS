import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['fixtures/**/*.test.ts'],
    exclude: ['tests/**', 'node_modules/**'],
  },
});
