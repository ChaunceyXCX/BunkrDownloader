import { fileURLToPath, URL } from 'node:url'
import { writeFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import type { Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// `emptyOutDir` wipes dist/ on every build, which would also delete the tracked
// `dist/.gitkeep` that main.go's `//go:embed all:frontend/dist` needs on a fresh
// checkout. Re-create it in-process (cross-platform; a `touch` shell command
// does not exist on Windows).
function keepGitkeep(): Plugin {
  return {
    name: 'keep-dist-gitkeep',
    apply: 'build',
    closeBundle() {
      writeFileSync(fileURLToPath(new URL('./dist/.gitkeep', import.meta.url)), '')
    },
  }
}

// BunkrDownloader frontend build.
//
// `base` is "/" (root absolute) rather than "./": the SPA uses history routing,
// so on a deep link such as /app/tasks/3 a relative "./assets/x.js" would
// resolve to /app/assets/x.js and 404. Both hosts (the Gin server and the
// Wails asset handler) mount the app at the origin root.
export default defineConfig({
  base: '/',
  plugins: [vue(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    emptyOutDir: true,
    chunkSizeWarningLimit: 900,
  },
  // The desktop build talks to Go over Wails service bindings and Wails events:
  // there is no HTTP API to proxy in `wails3 dev` anymore.
  server: {
    port: 5173,
  },
})
