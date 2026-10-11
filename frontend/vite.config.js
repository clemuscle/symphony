import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  // En dev, l'API et la connexion passent par le serveur Vite : même origine,
  // donc le cookie de session fonctionne comme en production
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
    },
  },
  build: {
    // Le front compilé est embarqué dans le binaire Go (internal/web, go:embed)
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
})
