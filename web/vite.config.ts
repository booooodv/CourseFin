import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const backendTarget = 'http://localhost:8096';

export default defineConfig({
  base: '/web/',
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': backendTarget,
      '/Items': backendTarget,
      '/Videos': backendTarget,
      '/System': backendTarget,
      '/emby': backendTarget,
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    environmentOptions: {
      jsdom: {
        url: 'http://localhost:5173/web/',
      },
    },
    setupFiles: './src/setupTests.ts',
    css: true,
  },
});
