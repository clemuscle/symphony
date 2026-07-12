<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Inventaire des ressources</h2>
        <p class="subtitle">Vue agrégée de toutes les ressources Symphony actives</p>
      </div>
      <button class="btn-refresh" @click="load" :disabled="loading">↻ Rafraîchir</button>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <!-- Summary cards -->
    <div class="summary-grid" v-if="data">
      <div class="summary-card">
        <div class="summary-val">{{ data.summary.projects }}</div>
        <div class="summary-label">Projets</div>
      </div>
      <div class="summary-card">
        <div class="summary-val">{{ data.summary.containers_running }}</div>
        <div class="summary-label">Containers actifs</div>
      </div>
      <div class="summary-card">
        <div class="summary-val">{{ data.summary.recettes_active }}</div>
        <div class="summary-label">Recettes actives</div>
      </div>
      <div class="summary-card">
        <div class="summary-val">{{ data.summary.pipelines_running }}</div>
        <div class="summary-label">Pipelines en cours</div>
      </div>
    </div>

    <div v-if="loading" class="skeleton-block"></div>

    <div v-else-if="data" class="sections">

      <!-- Projets -->
      <section class="section">
        <h3 class="section-title">🗂 Projets ({{ data.projects.length }})</h3>
        <div v-if="!data.projects.length" class="empty-row">Aucun projet</div>
        <table v-else class="resource-table">
          <thead>
            <tr>
              <th>Nom</th><th>Langage</th><th>Namespace</th><th>Statut</th><th>Repo</th><th>Créé le</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in data.projects" :key="p.id">
              <td class="name-cell">
                <span class="lang-icon">{{ langIcon(p.language) }}</span>
                {{ p.name }}
              </td>
              <td><span class="tag lang">{{ p.language }}</span></td>
              <td class="dim">{{ p.namespace || '—' }}</td>
              <td><span :class="['badge', p.status]">{{ p.status }}</span></td>
              <td>
                <a v-if="p.repo_url" :href="p.repo_url" target="_blank" class="link">↗ Repo</a>
                <span v-else class="dim">—</span>
              </td>
              <td class="dim">{{ formatDate(p.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Containers actifs -->
      <section class="section">
        <h3 class="section-title">🐳 Containers actifs ({{ data.containers.length }})</h3>
        <div v-if="!data.containers.length" class="empty-row">Aucun container en cours</div>
        <table v-else class="resource-table">
          <thead>
            <tr><th>Projet</th><th>Image</th><th>Port</th><th>Statut</th><th>Depuis</th></tr>
          </thead>
          <tbody>
            <tr v-for="c in data.containers" :key="c.id">
              <td class="name-cell">{{ c.project_name }}</td>
              <td class="dim mono">{{ shortImage(c.image) }}</td>
              <td>
                <a v-if="c.url" :href="c.url" target="_blank" class="link">:{{ c.port }} ↗</a>
                <span v-else class="dim">:{{ c.port }}</span>
              </td>
              <td><span :class="['badge', c.status]">{{ c.status }}</span></td>
              <td class="dim">{{ formatDate(c.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Recettes actives -->
      <section class="section">
        <h3 class="section-title">🧪 Recettes actives ({{ data.recettes.length }})</h3>
        <div v-if="!data.recettes.length" class="empty-row">Aucune recette en cours</div>
        <table v-else class="resource-table">
          <thead>
            <tr><th>Projet</th><th>Nom recette</th><th>Port</th><th>Statut</th><th>URL</th><th>Depuis</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in data.recettes" :key="r.id">
              <td class="name-cell">{{ r.project_name }}</td>
              <td>{{ r.recette_name }}</td>
              <td class="dim">:{{ r.port }}</td>
              <td><span :class="['badge', r.status]">{{ r.status }}</span></td>
              <td>
                <a v-if="r.url" :href="r.url" target="_blank" class="link">↗ Ouvrir</a>
                <span v-else class="dim">—</span>
              </td>
              <td class="dim">{{ formatDate(r.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Pipelines en cours -->
      <section class="section">
        <h3 class="section-title">⚙️ Pipelines en cours ({{ data.pipelines.length }})</h3>
        <div v-if="!data.pipelines.length" class="empty-row">Aucun pipeline actif</div>
        <table v-else class="resource-table">
          <thead>
            <tr><th>Projet</th><th>Pipeline ID</th><th>Type</th><th>Statut</th><th>Déclenché par</th><th>Depuis</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in data.pipelines" :key="p.id">
              <td class="name-cell">{{ p.project_name }}</td>
              <td class="dim mono">#{{ p.pipeline_id }}</td>
              <td><span class="tag type">{{ p.type }}</span></td>
              <td><span :class="['badge', p.status]">{{ p.status }}</span></td>
              <td class="dim">{{ p.triggered_by || '—' }}</td>
              <td class="dim">{{ formatDate(p.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Containers Docker live (bonus) -->
      <section class="section" v-if="data.live_containers?.length">
        <h3 class="section-title">🔴 Containers Docker live ({{ data.live_containers.length }})</h3>
        <p class="section-sub">État direct depuis le daemon Docker — peut inclure des containers non gérés par Symphony.</p>
        <table class="resource-table">
          <thead>
            <tr><th>ID</th><th>Nom</th><th>Image</th><th>Port exposé</th><th>Statut</th></tr>
          </thead>
          <tbody>
            <tr v-for="c in data.live_containers" :key="c.id">
              <td class="dim mono">{{ c.id }}</td>
              <td class="name-cell">{{ c.name }}</td>
              <td class="dim mono">{{ shortImage(c.image) }}</td>
              <td class="dim">{{ c.port ? ':' + c.port : '—' }}</td>
              <td class="dim">{{ c.status }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

const data = ref(null)
const loading = ref(true)
const error = ref(null)

const langIcon = (l) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[l] || '📦'

function shortImage(img) {
  if (!img) return '—'
  const parts = img.split('/')
  return parts[parts.length - 1]
}

function formatDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleDateString('fr-FR', { day: '2-digit', month: '2-digit', year: '2-digit' })
    + ' ' + d.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' })
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const { data: d } = await api.getInventory()
    data.value = d
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.btn-refresh { padding: 8px 16px; background: white; border: 1px solid #ddd; border-radius: 8px; cursor: pointer; font-size: 14px; }
.btn-refresh:disabled { opacity: .5; cursor: not-allowed; }
.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
  margin-bottom: 28px;
}
.summary-card {
  background: white;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  padding: 20px 24px;
  text-align: center;
}
.summary-val { font-size: 32px; font-weight: 700; color: #1a1a1a; }
.summary-label { font-size: 13px; color: #888; margin-top: 4px; }

.skeleton-block { height: 200px; background: linear-gradient(90deg,#f0f2f5 25%,#e8eaf0 50%,#f0f2f5 75%); background-size: 200% 100%; animation: shimmer 1.4s infinite; border-radius: 12px; }
@keyframes shimmer { 0%{background-position:200% 0} 100%{background-position:-200% 0} }

.sections { display: flex; flex-direction: column; gap: 24px; }

.section { background: white; border: 1px solid #e5e7eb; border-radius: 12px; overflow: hidden; }
.section-title { font-size: 15px; font-weight: 700; padding: 16px 20px; border-bottom: 1px solid #f0f0f0; }
.section-sub { font-size: 12px; color: #aaa; padding: 0 20px 12px; }
.empty-row { padding: 20px; color: #aaa; font-size: 14px; }

.resource-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.resource-table th { background: #f8f9fb; color: #888; font-weight: 600; padding: 10px 16px; text-align: left; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
.resource-table td { padding: 10px 16px; border-top: 1px solid #f5f5f5; }
.resource-table tr:hover td { background: #fafbfc; }

.name-cell { font-weight: 600; color: #1a1a1a; }
.lang-icon { margin-right: 6px; }
.dim { color: #888; }
.mono { font-family: monospace; font-size: 12px; }

.badge { display: inline-block; font-size: 11px; font-weight: 600; padding: 2px 9px; border-radius: 20px; }
.badge.ready, .badge.running, .badge.success { background: #f0fff4; color: #276749; }
.badge.pending { background: #eff6ff; color: #1d4ed8; }
.badge.degraded { background: #fffbeb; color: #92400e; }
.badge.failed, .badge.stopped { background: #fff5f5; color: #c53030; }
.badge.provisioning { background: #eff6ff; color: #1d4ed8; }

.tag { display: inline-block; font-size: 11px; padding: 2px 8px; border-radius: 20px; font-weight: 500; }
.tag.lang { background: #eef2ff; color: #667eea; }
.tag.type { background: #f4f4f4; color: #666; }

.link { color: #667eea; text-decoration: none; font-size: 13px; }
.link:hover { text-decoration: underline; }
</style>
