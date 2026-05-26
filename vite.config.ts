import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  publicDir: false,
  define: {
    __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
  },
  build: {
    outDir: 'public',
    emptyOutDir: false,
    sourcemap: true,
    // Bundle is ~830 KB unsplit because video.js + hls.js are bulky. Splitting
    // would force hashed chunk names that the service-worker pre-cache list
    // can't reference cleanly; on a LAN app the cost is one-time and SW-cached
    // afterwards, so the public-CDN-tuned 500 KB warning isn't actionable.
    chunkSizeWarningLimit: 1024,
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
