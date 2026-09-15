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
  build: {
    outDir: directory('../static/generated'),
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022',
    lib: {
      entry: directory('./src/main.tsx'),
      formats: ['es'],
      fileName: () => 'app.js',
      cssFileName: 'app',
    },
    rolldownOptions: { output: { codeSplitting: false } },
  },
}));
