<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">{{ projects.length }} projet(s) créé(s) via Symphony</p>
      </div>
      <RouterLink to="/" class="btn-new">+ Nouveau projet</RouterLink>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <!-- Skeleton -->
    <div v-if="loading" class="projects-grid">
      <div class="project-card skeleton" v-for="i in 3" :key="i"></div>
    </div>

    <!-- Liste -->
    <div class="projects-grid" v-else-if="projects.length">
      <div class="project-card" v-for="p in projects" :key="p.id">
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

        <div class="project-footer">
          <a v-if="p.repo_url" :href="p.repo_url" target="_blank" class="btn-ghost">
            Repo ↗
          </a>
          <button class="btn-ghost" @click="triggerPipeline(p)">
            {{ pipelineState[p.repo_path]?.loading ? '⏳' : '▶ Pipeline' }}
          </button>
          <button
            class="btn-ghost"
            :disabled="!p.registry_url || deployState[p.name]?.loading"
            :title="!p.registry_url ? 'Image non disponible' : ''"
            @click="deployProject(p)"
          >
            {{ deployState[p.name]?.loading ? '⏳' : '🚀 Déployer' }}
          </button>
        </div>

        <div class="pipeline-status" v-if="pipelineState[p.repo_path]?.id">
          <span class="pipeline-label">Pipeline #{{ pipelineState[p.repo_path].id }}</span>
          <span :class="['status-badge', 'sm', pipelineState[p.repo_path].status]">
            {{ pipelineState[p.repo_path].status }}
          </span>
        </div>

        <div class="deploy-status" v-if="deployState[p.name]?.status || deployState[p.name]?.error">
          <span class="pipeline-label">Déploiement</span>
          <span v-if="deployState[p.name]?.error" class="status-badge sm failed">{{ deployState[p.name].error }}</span>
          <span v-else :class="['status-badge', 'sm', deployState[p.name].status]">{{ deployState[p.name].status }}</span>
        </div>
      </div>
    </div>

    <!-- Empty state -->
    <div class="empty" v-else>
      <div class="empty-icon">🗂</div>
      <div class="empty-title">Aucun projet Symphony</div>
      <div class="empty-sub">Crée ton premier projet depuis le Catalogue</div>
      <RouterLink to="/" class="btn-cta">Explorer le catalogue →</RouterLink>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

const projects = ref([])
const loading = ref(true)
const error = ref(null)
const pipelineState = ref({})
const deployState = ref({})

const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const statusLabel = (s) => ({ ready: 'Prêt', provisioning: 'En cours', degraded: 'Dégradé', failed: 'Échec' })[s] || s

onMounted(async () => {
  try {
    const { data } = await api.listProjects()
    projects.value = data || []
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
})

async function deployProject(project) {
  deployState.value[project.name] = { loading: true }
  try {
    const { data } = await api.deploy({
      project_name: project.name,
      image: project.registry_url,
      port: project.port,
    })
    deployState.value[project.name] = { loading: false, status: data.status }
  } catch (e) {
    deployState.value[project.name] = { loading: false, error: e.response?.data?.error || e.message }
  }
}

async function triggerPipeline(project) {
  const path = project.repo_path
  if (!path) return
  pipelineState.value[path] = { loading: true }
  try {
    const { data } = await api.triggerPipeline(path, 'main', {})
    pipelineState.value[path] = { id: data.pipeline_id, status: 'pending', loading: false }
    pollStatus(path, data.pipeline_id)
  } catch (e) {
    pipelineState.value[path] = { error: e.response?.data?.error || e.message, loading: false }
  }
}

function pollStatus(path, id, attempts = 0) {
  if (attempts > 30) return
  setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(path, id)
      pipelineState.value[path] = { id, status: data.status, loading: false }
      if (!['success', 'failed', 'canceled'].includes(data.status))
        pollStatus(path, id, attempts + 1)
    } catch {}
  }, 5000)
}
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
  transition: box-shadow .15s;
}
.project-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,.06); }

.project-header { display: flex; justify-content: space-between; align-items: center; }
.project-title { display: flex; align-items: center; gap: 8px; }
.project-icon { font-size: 20px; }
.project-name { font-weight: 700; font-size: 15px; }

.project-desc { font-size: 13px; color: #777; line-height: 1.4; margin: 0; }

.project-meta { display: flex; flex-wrap: wrap; gap: 10px; }
.meta-item { font-size: 12px; color: #999; }

.project-footer { display: flex; gap: 8px; padding-top: 4px; border-top: 1px solid #f0f0f0; margin-top: 2px; }
.btn-ghost {
  padding: 6px 14px;
  background: transparent;
  border: 1px solid #e5e7eb;
  border-radius: 7px;
  cursor: pointer;
  font-size: 13px;
  color: #555;
  text-decoration: none;
  transition: all .15s;
}
.btn-ghost:hover { background: #f5f5f5; border-color: #ccc; }

.pipeline-status {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  background: #f8f9fb;
  border-radius: 8px;
  font-size: 12px;
}
.pipeline-label { color: #888; flex: 1; }

.deploy-status {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  background: #f0f4ff;
  border-radius: 8px;
  font-size: 12px;
}
button:disabled { opacity: 0.45; cursor: not-allowed; }

/* Status badges */
.status-badge {
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 20px;
  font-weight: 600;
  white-space: nowrap;
}
.status-badge.sm { font-size: 11px; padding: 2px 8px; }
.status-badge.ready { background: #f0fff4; color: #276749; }
.status-badge.provisioning, .status-badge.pending { background: #fffbeb; color: #92400e; }
.status-badge.degraded { background: #fffaf0; color: #c05621; }
.status-badge.failed { background: #fff5f5; color: #c53030; }
.status-badge.success { background: #f0fff4; color: #276749; }
.status-badge.running { background: #ebf8ff; color: #2b6cb0; }
.status-badge.canceled { background: #f4f4f4; color: #888; }

/* Skeleton */
.project-card.skeleton {
  min-height: 180px;
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
