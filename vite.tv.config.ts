import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// A SEPARATE build, deliberately — not a second entry in vite.config.ts. Two
// entries in one rollup hoist whatever they share into a chunk with a hashed
// name, which does three unwanted things at once: it pulls video.js's CSS out
// of notflix.css (leaving the main player unstyled), it emits a filename the
// service worker's fixed precache list cannot reference, and it couples the two
// apps' build output. The cost of keeping them apart is one duplicated video.js
// on a TV that downloads it once over the LAN and then caches it.
export default defineConfig({
  plugins: [svelte()],
  publicDir: false,
  define: {
    __BUILD_TIME__: JSON.stringify(new Date().toISOString()),
  },
  build: {
    // A TV browser is old and not updateable. Vite's default target ('modules')
    // assumes a browser from the year it shipped; this floor is Safari 11 /
    // Chrome 61, the first engines with native ES modules, which is as low as a
    // <script type="module"> page can go without a nomodule/SystemJS twin. If
    // the panel predates that, the page will fail silently and the fix is a
    // legacy build, not a lower target here.
    target: ['es2017', 'safari11', 'chrome61'],
    outDir: 'public',
    emptyOutDir: false,
    sourcemap: true,
    chunkSizeWarningLimit: 1024,
    rollupOptions: {
      input: 'src/tv/main.ts',
      output: {
        entryFileNames: 'assets/tv.js',
        chunkFileNames: 'assets/tv-[name]-[hash].js',
        assetFileNames: (info) =>
          info.name?.endsWith('.css') ? 'assets/tv.css' : 'assets/[name][extname]',
      },
    },
  },
  optimizeDeps: {
    include: ['video.js'],
  },
})
