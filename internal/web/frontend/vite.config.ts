import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';

const directory = (path: string) => fileURLToPath(new URL(path, import.meta.url));

// The development workbench has fictional data and no backend proxy. Production
// remains self-contained and same-origin, with the Go manager's unchanged CSP.
export default defineConfig(({ command }) => ({
  plugins: [react(), {
    name: 'snow-workbench-styles',
    apply: 'serve',
    transformIndexHtml() {
      const template = readFileSync(directory('../templates/pages.html'), 'utf8');
      const head = template.split('{{end}}', 1)[0];
      return [...head.matchAll(/href="\/static\/([^"]+\.css)"/g)].map(([, name]) => ({
        tag: 'link', attrs: { rel: 'stylesheet', href: `/@fs/${directory(`../static/${name}`)}` }, injectTo: 'head' as const,
      }));
    },
  }],
  publicDir: false,
  envPrefix: [],
  define: { 'process.env.NODE_ENV': JSON.stringify(command === 'build' ? 'production' : 'development') },
  base: command === 'build' ? '/static/generated/' : '/',
  server: { host: '127.0.0.1', fs: { allow: [directory('.'), directory('../static')] } },
  // The dictation engine runs in a module worker with a WebAssembly asset;
  // both land flat in the generated folder under names the Go allowlist admits.
  worker: { format: 'es', rollupOptions: { output: { entryFileNames: 'worker-[hash].js', chunkFileNames: 'worker-[name]-[hash].js', assetFileNames: 'app.[ext]' } } },
  build: {
    outDir: directory('../static/generated'),
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022',
    // An application build, not library mode: library-mode ES output keeps
    // whitespace and pure annotations, which left this bundle at ~860 KB. The
    // manager serves it on every page, so minify fully; gzip applies on top.
    minify: 'oxc',
    rolldownOptions: {
      input: directory('./src/main.tsx'),
      // Standalone pages load on demand as hashed chunks next to app.js; the Go
      // manager serves every generated .js file from the embedded tree.
      output: { format: 'es', entryFileNames: 'app.js', chunkFileNames: '[name]-[hash].js', assetFileNames: 'app.[ext]' },
    },
  },
}));
