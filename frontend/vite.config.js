import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  build: {
    // Le front compilé est embarqué dans le binaire Go (internal/web, go:embed)
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
})
