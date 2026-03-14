import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  publicDir: false,
  build: {
    outDir: 'public',
    emptyOutDir: false,
    sourcemap: true,
    rollupOptions: {
      input: 'src/main.ts',
      output: {
        entryFileNames: 'assets/notflix.js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: (info) =>
          info.name?.endsWith('.css') ? 'assets/notflix.css' : 'assets/[name][extname]',
      },
    },
  },
  optimizeDeps: {
    include: ['video.js'],
  },
})
