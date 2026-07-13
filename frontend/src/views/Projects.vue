<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">{{ projects.length }} projet(s) créé(s) via Symphony</p>
      </div>
      <RouterLink v-if="canDevelop" to="/" class="btn-new">+ Nouveau projet</RouterLink>
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
          <a v-if="p.repo_url" :href="p.repo_url + '/-/pipelines'" target="_blank" class="btn-ghost">
            Pipelines ↗
          </a>
          <a v-if="monitoringURLs[p.language]" :href="monitoringURLs[p.language]" target="_blank" class="btn-ghost btn-metrics">
            📊 Métriques ↗
          </a>
          <template v-if="canDevelop">
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
          </template>
        </div>

        <div class="pipeline-status" v-if="pipelineState[p.repo_path]?.id">
          <span class="pipeline-label">Pipeline #{{ pipelineState[p.repo_path].id }}</span>
          <span :class="['status-badge', 'sm', pipelineState[p.repo_path].status]">
            {{ pipelineState[p.repo_path].status }}
          </span>
        </div>

        <div class="deploy-status" v-if="deployState[p.name]?.loading || deployState[p.name]?.error || (deploymentMap[p.name] && deploymentMap[p.name].status !== 'stopped')">
          <span class="pipeline-label">Déploiement</span>
          <template v-if="deployState[p.name]?.loading">
            <span class="status-badge sm pending">⏳ lancement…</span>
          </template>
          <template v-else-if="deployState[p.name]?.error">
            <span class="status-badge sm failed">{{ deployState[p.name].error }}</span>
          </template>
          <template v-else-if="deploymentMap[p.name]">
            <a
              v-if="deploymentMap[p.name].status === 'running'"
              :href="deploymentMap[p.name].url || `http://localhost:${deploymentMap[p.name].port}`"
              target="_blank"
              class="deploy-link"
            >:{{ deploymentMap[p.name].port }} ↗</a>
            <span :class="['status-badge', 'sm', deploymentMap[p.name].status]">
              {{ deploymentMap[p.name].status }}
            </span>
            <button
              v-if="canDevelop && deploymentMap[p.name].status === 'running'"
              class="btn-ghost btn-xs btn-danger"
              @click="stopDeploy(deploymentMap[p.name])"
            >⏹</button>
          </template>
        </div>

        <button
          v-if="p.status === 'degraded' || p.status === 'failed'"
          class="btn-ghost btn-steps"
          @click="toggleSteps(p)"
        >
          {{ stepsState[p.name]?.open ? '▲ Masquer' : '▼ Détails provisioning' }}
        </button>

        <div class="steps-list" v-if="stepsState[p.name]?.open">
          <div v-if="stepsState[p.name]?.loading" class="step-row">Chargement…</div>
          <div v-for="s in stepsState[p.name]?.steps" :key="s.step" class="step-row">
            <span class="step-name">{{ s.step }}</span>
            <span :class="['step-status', s.status]">{{ s.status }}</span>
            <span v-if="s.error_detail" class="step-error">{{ s.error_detail }}</span>
          </div>
        </div>

        <!-- Recettes -->
        <div class="recette-section">
          <div class="recette-header" @click="toggleRecettes(p)">
            <span class="recette-title">🧪 Recettes</span>
            <span class="recette-count" v-if="recetteState[p.name]?.list?.length">
              {{ recetteState[p.name].list.length }}
            </span>
            <span class="recette-toggle">{{ recetteState[p.name]?.open ? '▲' : '▼' }}</span>
          </div>

          <div v-if="recetteState[p.name]?.open" class="recette-body">
            <div class="recette-list" v-if="recetteState[p.name]?.list?.length">
              <div v-for="rec in recetteState[p.name].list" :key="rec.recette_name" class="recette-row">
                <span class="recette-name">{{ rec.recette_name }}</span>
                <span class="recette-port">:{{ rec.port }}</span>
                <span :class="['status-badge', 'sm', rec.status]">{{ rec.status }}</span>
                <a v-if="rec.url" :href="rec.url" target="_blank" class="btn-ghost btn-xs">↗</a>
                <button
                  v-if="canDevelop"
                  class="btn-ghost btn-xs btn-danger"
                  :disabled="recetteState[p.name]?.destroying === rec.recette_name"
                  @click="destroyRecette(p, rec.recette_name)"
                >
                  {{ recetteState[p.name]?.destroying === rec.recette_name ? '⏳' : '✕' }}
                </button>
              </div>
            </div>
            <div v-else-if="!recetteState[p.name]?.loading" class="recette-empty">
              Aucune recette active
            </div>

            <div class="recette-form" v-if="canDevelop && !recetteState[p.name]?.creating">
              <button class="btn-ghost btn-sm" @click="startCreateRecette(p)">+ Nouvelle recette</button>
            </div>
            <div class="recette-create" v-else>
              <input
                v-model="recetteState[p.name].newName"
                placeholder="nom-recette"
                class="input-sm"
                @keyup.enter="submitRecette(p)"
              />
              <input
                v-model.number="recetteState[p.name].newPort"
                type="number"
                placeholder="port"
                class="input-sm input-port"
                @keyup.enter="submitRecette(p)"
              />
              <button class="btn-ghost btn-sm" @click="submitRecette(p)" :disabled="recetteState[p.name]?.submitting">
                {{ recetteState[p.name]?.submitting ? '⏳' : 'Déployer' }}
              </button>
              <button class="btn-ghost btn-sm" @click="cancelCreateRecette(p)">Annuler</button>
              <span v-if="recetteState[p.name]?.error" class="recette-error">{{ recetteState[p.name].error }}</span>
            </div>
          </div>
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
import { ref, onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const { canDevelop } = useAuth()

const projects = ref([])
const loading = ref(true)
const error = ref(null)
const pipelineState = ref({})
const deployState = ref({})
const deploymentMap = ref({}) // project_name → dernier déploiement (source DB)
const stepsState = ref({})
const recetteState = ref({})
const monitoringURLs = ref({}) // language → monitoring_url depuis les golden paths
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
  try {
    const { data } = await api.listDeployments()
    const map = {}
    for (const d of data || []) {
      if (!map[d.project_name]) map[d.project_name] = d // premier = plus récent (DESC)
    }
    deploymentMap.value = map
  } catch { /* non bloquant */ }
}

onMounted(async () => {
  load(true)
  pollInterval = setInterval(load, 10000)
  try {
    const { data } = await api.getGoldenPaths()
    const map = {}
    for (const gp of data || []) {
      if (gp.spec?.monitoring_url) map[gp.spec.language] = gp.spec.monitoring_url
    }
    monitoringURLs.value = map
  } catch { /* non bloquant */ }
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})

async function deployProject(project) {
  deployState.value[project.name] = { loading: true }
  try {
    const { data } = await api.deploy({ project_name: project.name })
    deploymentMap.value[project.name] = data
    delete deployState.value[project.name]
  } catch (e) {
    deployState.value[project.name] = { loading: false, error: e.response?.data?.error || e.message }
  }
}

async function stopDeploy(deployment) {
  try {
    await api.stopDeployment(deployment.id)
    deploymentMap.value[deployment.project_name] = { ...deployment, status: 'stopped' }
  } catch { /* non bloquant */ }
}

async function toggleSteps(project) {
  const name = project.name
  if (stepsState.value[name]?.open) {
    stepsState.value[name] = { ...stepsState.value[name], open: false }
    return
  }
  stepsState.value[name] = { loading: true, open: true }
  try {
    const { data } = await api.listProjectSteps(name)
    stepsState.value[name] = { loading: false, open: true, steps: data }
  } catch (e) {
    stepsState.value[name] = { loading: false, open: true, steps: [] }
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

async function toggleRecettes(project) {
  const name = project.name
  const cur = recetteState.value[name] || {}
  if (cur.open) {
    recetteState.value[name] = { ...cur, open: false }
    return
  }
  recetteState.value[name] = { ...cur, open: true, loading: true }
  try {
    const { data } = await api.listRecettes(name)
    recetteState.value[name] = { ...recetteState.value[name], loading: false, list: data || [] }
  } catch {
    recetteState.value[name] = { ...recetteState.value[name], loading: false, list: [] }
  }
}

function startCreateRecette(project) {
  const name = project.name
  recetteState.value[name] = {
    ...recetteState.value[name],
    creating: true,
    newName: '',
    newPort: null,
    error: null,
  }
}

function cancelCreateRecette(project) {
  const name = project.name
  recetteState.value[name] = { ...recetteState.value[name], creating: false, error: null }
}

async function submitRecette(project) {
  const name = project.name
  const state = recetteState.value[name] || {}
  if (!state.newName || !state.newPort) {
    recetteState.value[name] = { ...state, error: 'Nom et port requis' }
    return
  }
  recetteState.value[name] = { ...state, submitting: true, error: null }
  try {
    await api.createRecette(name, { recette_name: state.newName, port: state.newPort })
    const { data } = await api.listRecettes(name)
    recetteState.value[name] = {
      ...recetteState.value[name],
      submitting: false,
      creating: false,
      list: data || [],
    }
  } catch (e) {
    recetteState.value[name] = {
      ...recetteState.value[name],
      submitting: false,
      error: e.response?.data?.error || e.message,
    }
  }
}

async function destroyRecette(project, recetteName) {
  const name = project.name
  recetteState.value[name] = { ...recetteState.value[name], destroying: recetteName }
  try {
    await api.destroyRecette(name, recetteName)
    const { data } = await api.listRecettes(name)
    recetteState.value[name] = { ...recetteState.value[name], destroying: null, list: data || [] }
  } catch (e) {
    recetteState.value[name] = { ...recetteState.value[name], destroying: null }
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
.status-badge.stopped { background: #f4f4f4; color: #888; }

.deploy-link { color: #2b6cb0; font-family: monospace; font-size: 11px; text-decoration: none; }
.deploy-link:hover { text-decoration: underline; }

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

.btn-steps { font-size: 12px; padding: 4px 10px; color: #c05621; border-color: #fed7aa; }
.btn-metrics { color: #6b7280; }
.steps-list { background: #fafafa; border: 1px solid #e5e7eb; border-radius: 8px; padding: 8px; font-size: 12px; }
.step-row { display: flex; align-items: baseline; gap: 8px; padding: 3px 0; }
.step-name { font-weight: 600; color: #555; min-width: 80px; }
.step-status { padding: 1px 7px; border-radius: 10px; font-size: 11px; font-weight: 600; }
.step-status.success { background: #f0fff4; color: #276749; }
.step-status.failed { background: #fff5f5; color: #c53030; }
.step-error { color: #c53030; font-size: 11px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* Recettes */
.recette-section { border: 1px solid #e5e7eb; border-radius: 8px; overflow: hidden; font-size: 13px; }
.recette-header { display: flex; align-items: center; gap: 6px; padding: 7px 10px; background: #f8f9fb; cursor: pointer; user-select: none; }
.recette-header:hover { background: #f0f2f7; }
.recette-title { font-weight: 600; color: #555; flex: 1; }
.recette-count { background: #e0e7ff; color: #4338ca; font-size: 11px; font-weight: 700; padding: 1px 6px; border-radius: 10px; }
.recette-toggle { color: #aaa; font-size: 10px; }
.recette-body { padding: 8px 10px; display: flex; flex-direction: column; gap: 8px; }
.recette-list { display: flex; flex-direction: column; gap: 4px; }
.recette-row { display: flex; align-items: center; gap: 6px; padding: 4px 0; border-bottom: 1px solid #f0f0f0; }
.recette-row:last-child { border-bottom: none; }
.recette-name { font-weight: 600; color: #444; flex: 1; font-size: 12px; }
.recette-port { color: #888; font-size: 11px; font-family: monospace; }
.recette-empty { color: #aaa; font-size: 12px; padding: 4px 0; }
.recette-error { color: #c53030; font-size: 11px; }
.recette-form { display: flex; gap: 6px; }
.recette-create { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.input-sm {
  padding: 4px 8px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  font-size: 12px;
  outline: none;
  flex: 1;
  min-width: 80px;
}
.input-sm:focus { border-color: #667eea; }
.input-port { max-width: 80px; flex: none; }
.btn-sm { font-size: 12px; padding: 4px 10px; }
.btn-xs { font-size: 11px; padding: 2px 7px; }
.btn-danger { color: #c53030; border-color: #feb2b2; }
.btn-danger:hover { background: #fff5f5; }
</style>
