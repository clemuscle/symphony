<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">{{ projects.length }} projet(s) créé(s) via Symphony</p>
      </div>
      <RouterLink v-if="canDevelop" to="/catalogue" class="btn-new">+ Nouveau projet</RouterLink>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <!-- Skeleton -->
    <div v-if="loading" class="projects-grid">
      <div class="project-card skeleton" v-for="i in 3" :key="i"></div>
    </div>

    <!-- Liste compacte -->
    <div class="projects-grid" v-else-if="projects.length">
      <RouterLink
        v-for="p in projects"
        :key="p.id"
        :to="`/projects/${encodeURIComponent(p.name)}`"
        class="project-card"
      >
        <div class="project-header">
          <div class="project-title">
            <span class="project-icon">{{ langIcon(p.language) }}</span>
            <span class="project-name">{{ p.name }}</span>
          </div>
          <span :class="['status-badge', p.status]">{{ statusLabel(p.status) }}</span>
        </div>

        <p class="project-desc">{{ p.description || 'Pas de description' }}</p>

        <div class="project-meta">
          <span class="meta-item">📁 {{ p.repo_path || '—' }}</span>
          <span class="meta-item">🔌 :{{ p.port }}</span>
          <span class="meta-item" v-if="p.namespace">📂 {{ p.namespace }}</span>
        </div>

        <span class="detail-hint">Voir les détails →</span>
      </RouterLink>
    </div>

    <!-- Empty state -->
    <div class="empty" v-else>
      <div class="empty-icon">🗂</div>
      <div class="empty-title">Aucun projet Symphony</div>
      <div class="empty-sub">Crée ton premier projet depuis le Catalogue</div>
      <RouterLink to="/catalogue" class="btn-cta">Explorer le catalogue →</RouterLink>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const { canDevelop } = useAuth()

const projects = ref([])
const loading = ref(true)
const error = ref(null)
let pollInterval = null

const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const statusLabel = (s) => ({ ready: 'Prêt', provisioning: 'En cours', degraded: 'Dégradé', failed: 'Échec' })[s] || s

async function load(isInitial = false) {
  if (isInitial) loading.value = true
  try {
    const { data } = await api.listProjects()
    projects.value = data || []
    error.value = null
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load(true)
  pollInterval = setInterval(() => load(false), 10000)
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 28px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }

.btn-new {
  background: #667eea;
  color: white;
  border: none;
  border-radius: 8px;
  padding: 9px 20px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  text-decoration: none;
  transition: background .15s;
}
.btn-new:hover { background: #5a6fd6; }

.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.projects-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 16px; }

.project-card {
  background: white;
  border: 1.5px solid #e5e7eb;
  border-radius: 14px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition: box-shadow .15s, border-color .15s;
  text-decoration: none;
  color: inherit;
}
.project-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,.06); border-color: #667eea55; }

.project-header { display: flex; justify-content: space-between; align-items: center; }
.project-title { display: flex; align-items: center; gap: 8px; }
.project-icon { font-size: 20px; }
.project-name { font-weight: 700; font-size: 15px; color: #1a1a1a; }

.project-desc { font-size: 13px; color: #777; line-height: 1.4; margin: 0; }

.project-meta { display: flex; flex-wrap: wrap; gap: 10px; }
.meta-item { font-size: 12px; color: #999; }

.detail-hint { font-size: 12px; color: #667eea; font-weight: 600; margin-top: auto; padding-top: 4px; }

/* Status badges */
.status-badge {
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 20px;
  font-weight: 600;
  white-space: nowrap;
}
.status-badge.ready { background: #f0fff4; color: #276749; }
.status-badge.provisioning, .status-badge.pending { background: #fffbeb; color: #92400e; }
.status-badge.degraded { background: #fffaf0; color: #c05621; }
.status-badge.failed { background: #fff5f5; color: #c53030; }

/* Skeleton */
.project-card.skeleton {
  min-height: 140px;
  background: linear-gradient(90deg, #f0f2f5 25%, #e8eaf0 50%, #f0f2f5 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s infinite;
  border: none;
  pointer-events: none;
}
@keyframes shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }

/* Empty state */
.empty { text-align: center; padding: 80px 40px; }
.empty-icon { font-size: 48px; margin-bottom: 16px; }
.empty-title { font-size: 16px; font-weight: 700; color: #444; margin-bottom: 6px; }
.empty-sub { font-size: 13px; color: #888; margin-bottom: 20px; }
.btn-cta {
  display: inline-block;
  background: #667eea;
  color: white;
  text-decoration: none;
  border-radius: 8px;
  padding: 10px 22px;
  font-size: 14px;
  font-weight: 600;
  transition: background .15s;
}
.btn-cta:hover { background: #5a6fd6; }
</style>
