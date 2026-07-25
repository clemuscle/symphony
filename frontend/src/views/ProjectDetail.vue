<template>
  <div>
    <RouterLink to="/" class="back-link">← Projets</RouterLink>

    <div v-if="loading" class="skeleton-header"></div>

    <div v-else-if="notFound" class="empty">
      <div class="empty-icon">🗂</div>
      <div class="empty-title">Projet introuvable</div>
      <div class="empty-sub">« {{ projectName }} » n'existe pas ou plus.</div>
      <RouterLink to="/" class="btn-cta">Retour aux projets →</RouterLink>
    </div>

    <template v-else>
      <div class="page-header">
        <div class="project-title">
          <span class="project-icon">{{ langIcon(project.language) }}</span>
          <h2>{{ project.name }}</h2>
          <span :class="['status-badge', project.status]">{{ statusLabel(project.status) }}</span>
        </div>
        <p class="project-desc">{{ project.description || 'Pas de description' }}</p>
        <div class="project-meta">
          <span class="meta-item" v-if="project.repo_path">📁 {{ project.repo_path }}</span>
          <span class="meta-item">🔌 :{{ project.port }}</span>
          <span class="meta-item" v-if="project.namespace">📂 {{ project.namespace }}</span>
        </div>
        <div class="project-links">
          <a v-if="project.repo_url" :href="project.repo_url" target="_blank" class="btn-ghost">Repo ↗</a>
          <a v-if="project.repo_url" :href="project.repo_url + '/-/pipelines'" target="_blank" class="btn-ghost">Pipelines ↗</a>
          <a v-if="monitoringURL" :href="monitoringURL" target="_blank" class="btn-ghost btn-metrics">📊 Métriques ↗</a>
        </div>
      </div>

      <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

      <!-- Pipeline -->
      <section class="card">
        <div class="card-title">🔧 Pipeline</div>
        <div class="card-body">
          <!-- canDeploy (lead+), pas canDevelop : sans variable, ce déclenchement
               route vers le job "deploy" du golden path (voir G1, backlog.md) —
               même niveau de droit que le bouton "Déployer" ci-dessous. -->
          <div class="inline-actions" v-if="canDeploy">
            <input
              v-model="pipelineBranch"
              placeholder="main"
              class="branch-input"
              @keyup.enter="triggerPipeline"
            />
            <button class="btn-ghost" @click="triggerPipeline">
              {{ pipelineState.loading ? '⏳' : '▶ Lancer pipeline' }}
            </button>
          </div>

          <div class="pipeline-status" v-if="pipelineState.id || pipelineState.loading">
            <span class="pipeline-label">Pipeline en cours</span>
            <template v-if="pipelineState.loading">
              <span class="status-badge sm pending">⏳ déclenchement…</span>
            </template>
            <template v-else-if="pipelineState.id">
              <span class="pipeline-id">#{{ pipelineState.id }}</span>
              <span :class="['status-badge', 'sm', pipelineState.status]">{{ pipelineState.status }}</span>
            </template>
          </div>

          <div class="job-list" v-if="recentPipelines.length">
            <div class="job-list-title">
              5 derniers jobs
              <span class="success-rate" v-if="pipelineSuccessRate">{{ pipelineSuccessRate }}</span>
            </div>
            <div v-for="p in recentPipelines.slice(0, 5)" :key="p.id" class="job-row">
              <a
                v-if="project.repo_url"
                :href="`${project.repo_url}/-/pipelines/${p.pipeline_id}`"
                target="_blank"
                class="pipeline-id pipeline-link"
              >#{{ p.pipeline_id }}</a>
              <span v-else class="pipeline-id">#{{ p.pipeline_id }}</span>
              <span :class="['status-badge', 'sm', p.status]">{{ p.status }}</span>
              <span class="job-date">{{ formatDate(p.created_at) }}</span>
            </div>
          </div>
          <div v-else class="empty-inline">Aucun pipeline lancé pour l'instant</div>
        </div>
      </section>

      <!-- Images -->
      <section class="card">
        <div class="card-title">📦 Images</div>
        <div class="card-body">
          <div class="image-list" v-if="images.length">
            <div v-for="img in images" :key="`${img.name}:${img.tag}`" class="image-row">
              <span class="image-name">{{ img.name }}</span>
              <span class="image-tag">{{ img.tag }}</span>
            </div>
          </div>
          <div v-else class="empty-inline">{{ imagesError || 'Aucune image poussée pour l’instant' }}</div>
        </div>
      </section>

      <!-- Branches -->
      <section class="card">
        <div class="card-title">🌿 Branches</div>
        <div class="card-body">
          <div class="branch-list" v-if="branches.length">
            <span
              v-for="b in branches.slice(0, 5)"
              :key="b.name"
              :class="['branch-chip', { default: b.default }]"
            >{{ b.name }}<template v-if="b.default"> · défaut</template></span>
          </div>
          <div v-else class="empty-inline">{{ branchesError || 'Aucune branche trouvée' }}</div>
        </div>
      </section>

      <!-- Déploiement -->
      <section class="card">
        <div class="card-title">🚀 Déploiement</div>
        <div class="card-body">
          <div class="inline-actions" v-if="canDeploy">
            <select v-model="deployEnvironment" class="env-select">
              <option value="prod">prod</option>
              <option value="preprod">preprod</option>
            </select>
            <button
              class="btn-ghost"
              :disabled="!project.registry_url || deployState.loading"
              :title="!project.registry_url ? 'Image non disponible' : ''"
              @click="deployProject"
            >
              {{ deployState.loading ? '⏳' : '🚀 Déployer' }}
            </button>
          </div>

          <div class="deploy-status" v-if="deployState.loading || deployState.error">
            <template v-if="deployState.loading">
              <span class="status-badge sm pending">⏳ lancement…</span>
            </template>
            <template v-else-if="deployState.error">
              <span class="status-badge sm failed">{{ deployState.error }}</span>
            </template>
          </div>

          <div class="env-deploy-list" v-if="Object.keys(deploymentsByEnv).length">
            <div v-for="(d, env) in deploymentsByEnv" :key="env" class="deploy-status">
              <span :class="['env-badge', env]">{{ env }}</span>
              <a
                v-if="d.status === 'running'"
                :href="d.url || `http://localhost:${d.port}`"
                target="_blank"
                class="deploy-link"
              >:{{ d.port }} ↗</a>
              <span :class="['status-badge', 'sm', d.status]">{{ d.status }}</span>
              <span class="deploy-image-ref" v-if="d.image" :title="d.image">🐳 {{ imageLabel(d.image) }}</span>
              <span class="tag-chip" v-for="t in d.tags" :key="t">{{ t }}</span>
              <button
                v-if="canDeploy && d.status === 'running'"
                class="btn-ghost btn-xs btn-danger"
                @click="stopDeploy(d)"
              >⏹ Arrêter</button>
            </div>
          </div>
          <div v-else class="empty-inline">Pas de déploiement actif</div>
        </div>
      </section>

      <!-- Provisioning (si dégradé/échec) -->
      <section class="card" v-if="project.status === 'degraded' || project.status === 'failed'">
        <div class="card-title" @click="toggleSteps" style="cursor:pointer">
          🛠 Détails provisioning {{ stepsOpen ? '▲' : '▼' }}
        </div>
        <div class="card-body" v-if="stepsOpen">
          <div v-if="stepsLoading" class="empty-inline">Chargement…</div>
          <div v-for="s in steps" :key="s.step" class="step-row">
            <span class="step-name">{{ s.step }}</span>
            <span :class="['step-status', s.status]">{{ s.status }}</span>
            <span v-if="s.error_detail" class="step-error">{{ s.error_detail }}</span>
          </div>
        </div>
      </section>

      <!-- Recettes -->
      <section class="card">
        <div class="card-title">🧪 Recettes <span class="recette-count" v-if="recettes.length">{{ recettes.length }}</span></div>
        <div class="card-body">
          <div class="recette-list" v-if="recettes.length">
            <div v-for="rec in recettes" :key="rec.recette_name" class="recette-row">
              <span class="recette-name">{{ rec.recette_name }}</span>
              <span class="recette-port">:{{ rec.port }}</span>
              <span :class="['status-badge', 'sm', rec.status]">{{ rec.status }}</span>
              <span class="tag-chip" v-for="t in rec.tags" :key="t">{{ t }}</span>
              <a v-if="rec.url" :href="rec.url" target="_blank" class="btn-ghost btn-xs">↗</a>
              <button
                v-if="canDevelop"
                class="btn-ghost btn-xs btn-danger"
                :disabled="recetteDestroying === rec.recette_name"
                @click="destroyRecette(rec.recette_name)"
              >{{ recetteDestroying === rec.recette_name ? '⏳' : '✕' }}</button>
            </div>
          </div>
          <div v-else-if="!recettesLoading" class="empty-inline">Aucune recette active</div>

          <div class="recette-form" v-if="canDevelop && !recetteCreating">
            <button class="btn-ghost btn-sm" @click="recetteCreating = true">+ Nouvelle recette</button>
          </div>
          <div class="recette-create" v-else-if="canDevelop">
            <input v-model="newRecetteName" placeholder="nom-recette" class="input-sm" @keyup.enter="submitRecette" />
            <input v-model.number="newRecettePort" type="number" placeholder="port" class="input-sm input-port" @keyup.enter="submitRecette" />
            <button class="btn-ghost btn-sm" @click="submitRecette" :disabled="recetteSubmitting">
              {{ recetteSubmitting ? '⏳' : 'Déployer' }}
            </button>
            <button class="btn-ghost btn-sm" @click="recetteCreating = false; recetteError = null">Annuler</button>
            <span v-if="recetteError" class="recette-error">{{ recetteError }}</span>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const route = useRoute()
const { canDevelop, canDeploy } = useAuth()

const projectName = computed(() => route.params.name)

const project = ref(null)
const loading = ref(true)
const notFound = ref(false)
const error = ref(null)

const pipelineState = ref({ loading: false, id: null, status: null })
const pipelineBranch = ref('main')
const recentPipelines = ref([])

const deployState = ref({ loading: false, error: null })
const deploymentsByEnv = ref({})
const deployEnvironment = ref('prod')

const stepsOpen = ref(false)
const stepsLoading = ref(false)
const steps = ref([])

const images = ref([])
const imagesError = ref(null)
const branches = ref([])
const branchesError = ref(null)

const recettes = ref([])
const recettesLoading = ref(false)
const recetteCreating = ref(false)
const newRecetteName = ref('')
const newRecettePort = ref(null)
const recetteError = ref(null)
const recetteSubmitting = ref(false)
const recetteDestroying = ref(null)

const monitoringURL = ref(null)

let pollInterval = null

const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const statusLabel = (s) => ({ ready: 'Prêt', provisioning: 'En cours', degraded: 'Dégradé', failed: 'Échec' })[s] || s
const formatDate = (d) => d ? new Date(d).toLocaleString('fr-FR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }) : ''
// Ne garde que le dernier segment du chemin (nom du repo/image) — le champ
// "image" est en pratique une URL de registre complète, pas juste un tag.
const imageLabel = (image) => image.split('/').filter(Boolean).pop() || image

// Panneau de statut opérationnel (B4) : taux de succès sur les pipelines
// déjà chargés pour "5 derniers jobs" — aucune nouvelle donnée, juste un
// agrégat de ce que Symphony sait déjà (pas une métrique Prometheus).
const pipelineSuccessRate = computed(() => {
  const sample = recentPipelines.value.slice(0, 10)
  if (!sample.length) return ''
  const successes = sample.filter(p => p.status === 'success').length
  return `${successes}/${sample.length} succès`
})

async function loadProject() {
  const { data } = await api.listProjects()
  const found = (data || []).find(p => p.name === projectName.value)
  if (!found) {
    notFound.value = true
    return null
  }
  project.value = found
  return found
}

async function loadPipeline(p) {
  if (!p.repo_path) return
  try {
    const { data } = await api.listPipelines(p.repo_path)
    recentPipelines.value = data || []
  } catch { /* non bloquant */ }
}

async function loadImages(p) {
  try {
    const { data } = await api.listProjectImages(p.name)
    images.value = data || []
    imagesError.value = null
  } catch (e) {
    images.value = []
    imagesError.value = e.response?.data?.error || null
  }
}

async function loadBranches(p) {
  try {
    const { data } = await api.listProjectBranches(p.name)
    branches.value = data || []
    branchesError.value = null
  } catch (e) {
    branches.value = []
    branchesError.value = e.response?.data?.error || null
  }
}

async function loadDeployment(p) {
  try {
    const { data } = await api.listDeployments()
    const byEnv = {}
    for (const d of data || []) {
      // Liste déjà triée DESC par created_at côté API — le premier
      // rencontré par environnement est le plus récent.
      if (d.project_name === p.name && !byEnv[d.environment]) byEnv[d.environment] = d
    }
    deploymentsByEnv.value = byEnv
  } catch { /* non bloquant */ }
}

async function loadRecettes(p) {
  recettesLoading.value = true
  try {
    const { data } = await api.listRecettes(p.name)
    recettes.value = data || []
  } catch {
    recettes.value = []
  } finally {
    recettesLoading.value = false
  }
}

async function loadMonitoring(p) {
  try {
    const { data } = await api.getGoldenPaths()
    const gp = (data || []).find(g => g.spec?.language === p.language)
    monitoringURL.value = gp?.spec?.monitoring_url || null
  } catch { /* non bloquant */ }
}

async function refresh(isInitial = false) {
  if (isInitial) loading.value = true
  try {
    const p = await loadProject()
    if (!p) return
    error.value = null
    await Promise.all([loadPipeline(p), loadDeployment(p), loadRecettes(p)])
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await refresh(true)
  if (project.value) {
    loadMonitoring(project.value)
    loadImages(project.value)
    loadBranches(project.value)
  }
  pollInterval = setInterval(() => refresh(false), 10000)
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})

async function triggerPipeline() {
  if (!project.value?.repo_path) return
  pipelineState.value = { loading: true }
  try {
    const branch = pipelineBranch.value || 'main'
    const { data } = await api.triggerPipeline(project.value.repo_path, branch, {})
    pipelineState.value = { id: data.pipeline_id, status: 'pending', loading: false }
    pollStatus(data.pipeline_id)
  } catch (e) {
    pipelineState.value = { error: e.response?.data?.error || e.message, loading: false }
  }
}

function pollStatus(id, attempts = 0) {
  if (attempts > 30 || !project.value?.repo_path) return
  setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(project.value.repo_path, id)
      pipelineState.value = { id, status: data.status, loading: false }
      if (!['success', 'failed', 'canceled'].includes(data.status)) pollStatus(id, attempts + 1)
    } catch {}
  }, 5000)
}

async function deployProject() {
  deployState.value = { loading: true }
  try {
    const { data } = await api.deploy({ project_name: project.value.name, environment: deployEnvironment.value })
    deploymentsByEnv.value = { ...deploymentsByEnv.value, [data.environment]: data }
    deployState.value = { loading: false, error: null }
  } catch (e) {
    deployState.value = { loading: false, error: e.response?.data?.error || e.message }
  }
}

async function stopDeploy(d) {
  try {
    // La destruction est déléguée au pipeline CI — le statut réel revient via
    // le polling de refresh(), pas ici.
    await api.stopDeployment(d.id)
  } catch { /* non bloquant */ }
}

async function toggleSteps() {
  stepsOpen.value = !stepsOpen.value
  if (!stepsOpen.value || steps.value.length) return
  stepsLoading.value = true
  try {
    const { data } = await api.listProjectSteps(project.value.name)
    steps.value = data || []
  } finally {
    stepsLoading.value = false
  }
}

async function submitRecette() {
  if (!newRecetteName.value || !newRecettePort.value) {
    recetteError.value = 'Nom et port requis'
    return
  }
  recetteSubmitting.value = true
  recetteError.value = null
  try {
    await api.createRecette(project.value.name, { recette_name: newRecetteName.value, port: newRecettePort.value })
    await loadRecettes(project.value)
    recetteCreating.value = false
    newRecetteName.value = ''
    newRecettePort.value = null
  } catch (e) {
    recetteError.value = e.response?.data?.error || e.message
  } finally {
    recetteSubmitting.value = false
  }
}

async function destroyRecette(recetteName) {
  recetteDestroying.value = recetteName
  try {
    await api.destroyRecette(project.value.name, recetteName)
    await loadRecettes(project.value)
  } finally {
    recetteDestroying.value = null
  }
}
</script>

<style scoped>
.back-link { display: inline-block; color: #888; text-decoration: none; font-size: 13px; margin-bottom: 16px; }
.back-link:hover { color: #555; }

.page-header { margin-bottom: 24px; }
.project-title { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
.project-icon { font-size: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.project-desc { font-size: 13px; color: #777; margin-bottom: 10px; }
.project-meta { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 10px; }
.meta-item { font-size: 12px; color: #999; }
.project-links { display: flex; gap: 8px; flex-wrap: wrap; }

.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.card { background: white; border: 1.5px solid #e5e7eb; border-radius: 14px; margin-bottom: 16px; overflow: hidden; }
.card-title { font-weight: 700; font-size: 14px; color: #1a1a2e; padding: 14px 20px; border-bottom: 1px solid #f0f0f0; display: flex; align-items: center; gap: 8px; }
.card-body { padding: 16px 20px; display: flex; flex-direction: column; gap: 12px; }

.inline-actions { display: flex; gap: 8px; align-items: center; }
.branch-input { padding: 6px 10px; border: 1px solid #e5e7eb; border-radius: 6px; font-size: 13px; width: 100px; outline: none; color: #555; }
.branch-input:focus { border-color: #667eea; }
.btn-ghost {
  padding: 6px 14px; background: transparent; border: 1px solid #e5e7eb; border-radius: 7px;
  cursor: pointer; font-size: 13px; color: #555; text-decoration: none; transition: all .15s;
}
.btn-ghost:hover { background: #f5f5f5; border-color: #ccc; }
.btn-ghost:disabled { opacity: 0.45; cursor: not-allowed; }
.btn-metrics { color: #6b7280; }

.pipeline-status, .deploy-status {
  display: flex; align-items: center; gap: 8px; padding: 7px 10px; border-radius: 8px; font-size: 12px;
}
.pipeline-status { background: #f8f9fb; }
.deploy-status { background: #f0f4ff; }
.env-deploy-list { display: flex; flex-direction: column; gap: 6px; }
.pipeline-label { color: #888; flex: 1; }
.pipeline-id { color: #666; font-family: monospace; font-size: 11px; }
.pipeline-link { color: #2b6cb0; text-decoration: none; }
.pipeline-link:hover { text-decoration: underline; }
.deploy-link { color: #2b6cb0; font-family: monospace; font-size: 11px; text-decoration: none; }
.deploy-link:hover { text-decoration: underline; }

.empty-inline { font-size: 13px; color: #aaa; }

.job-list { display: flex; flex-direction: column; gap: 4px; }
.job-list-title { font-size: 11px; font-weight: 700; color: #999; text-transform: uppercase; letter-spacing: .04em; margin-bottom: 2px; display: flex; align-items: center; justify-content: space-between; }
.success-rate { font-size: 11px; font-weight: 700; color: #276749; text-transform: none; letter-spacing: normal; }

.deploy-image-ref { font-size: 11px; color: #888; font-family: monospace; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.job-row { display: flex; align-items: center; gap: 10px; padding: 5px 0; border-bottom: 1px solid #f0f0f0; font-size: 12px; }
.job-row:last-child { border-bottom: none; }
.job-date { color: #aaa; margin-left: auto; }

.image-list { display: flex; flex-direction: column; gap: 4px; }
.image-row { display: flex; align-items: center; gap: 10px; padding: 5px 0; border-bottom: 1px solid #f0f0f0; font-size: 13px; }
.image-row:last-child { border-bottom: none; }
.image-name { font-weight: 600; color: #444; }
.image-tag { font-family: monospace; font-size: 12px; color: #667eea; background: #f0f2ff; padding: 1px 8px; border-radius: 10px; }

.branch-list { display: flex; flex-wrap: wrap; gap: 8px; }
.branch-chip { font-size: 12px; padding: 4px 12px; border-radius: 20px; background: #f4f4f7; color: #555; font-family: monospace; }
.branch-chip.default { background: #eef2ff; color: #4338ca; font-weight: 600; }

.env-select { padding: 6px 10px; border: 1px solid #e5e7eb; border-radius: 6px; font-size: 13px; color: #555; background: white; }
.env-select:focus { border-color: #667eea; outline: none; }

.env-badge { font-size: 11px; font-weight: 700; padding: 2px 9px; border-radius: 20px; text-transform: uppercase; letter-spacing: .03em; }
.env-badge.prod { background: #fef2f2; color: #b91c1c; }
.env-badge.preprod { background: #fffbeb; color: #92400e; }
.env-badge.recette { background: #eff6ff; color: #1d4ed8; }

.tag-chip { font-size: 11px; padding: 2px 8px; border-radius: 10px; background: #f4f4f7; color: #666; font-family: monospace; }

.status-badge { font-size: 12px; padding: 3px 10px; border-radius: 20px; font-weight: 600; white-space: nowrap; }
.status-badge.sm { font-size: 11px; padding: 2px 8px; }
.status-badge.ready { background: #f0fff4; color: #276749; }
.status-badge.provisioning, .status-badge.pending { background: #fffbeb; color: #92400e; }
.status-badge.degraded { background: #fffaf0; color: #c05621; }
.status-badge.failed { background: #fff5f5; color: #c53030; }
.status-badge.success { background: #f0fff4; color: #276749; }
.status-badge.running { background: #ebf8ff; color: #2b6cb0; }
.status-badge.canceled { background: #f4f4f4; color: #888; }
.status-badge.stopped { background: #f4f4f4; color: #888; }

.step-row { display: flex; align-items: baseline; gap: 8px; padding: 4px 0; font-size: 12px; }
.step-name { font-weight: 600; color: #555; min-width: 80px; }
.step-status { padding: 1px 7px; border-radius: 10px; font-size: 11px; font-weight: 600; }
.step-status.success { background: #f0fff4; color: #276749; }
.step-status.failed { background: #fff5f5; color: #c53030; }
.step-error { color: #c53030; font-size: 11px; flex: 1; }

.recette-count { background: #e0e7ff; color: #4338ca; font-size: 11px; font-weight: 700; padding: 1px 6px; border-radius: 10px; }
.recette-list { display: flex; flex-direction: column; gap: 4px; }
.recette-row { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px solid #f0f0f0; font-size: 13px; }
.recette-row:last-child { border-bottom: none; }
.recette-name { font-weight: 600; color: #444; flex: 1; }
.recette-port { color: #888; font-size: 12px; font-family: monospace; }
.recette-error { color: #c53030; font-size: 11px; }
.recette-form { display: flex; gap: 6px; }
.recette-create { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.input-sm { padding: 5px 10px; border: 1px solid #e5e7eb; border-radius: 6px; font-size: 12px; outline: none; flex: 1; min-width: 100px; }
.input-sm:focus { border-color: #667eea; }
.input-port { max-width: 90px; flex: none; }
.btn-sm { font-size: 12px; padding: 5px 12px; }
.btn-xs { font-size: 11px; padding: 2px 7px; }
.btn-danger { color: #c53030; border-color: #feb2b2; }
.btn-danger:hover { background: #fff5f5; }

.skeleton-header { height: 120px; border-radius: 14px; background: linear-gradient(90deg, #f0f2f5 25%, #e8eaf0 50%, #f0f2f5 75%); background-size: 200% 100%; animation: shimmer 1.4s infinite; }
@keyframes shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }

.empty { text-align: center; padding: 80px 40px; }
.empty-icon { font-size: 48px; margin-bottom: 16px; }
.empty-title { font-size: 16px; font-weight: 700; color: #444; margin-bottom: 6px; }
.empty-sub { font-size: 13px; color: #888; margin-bottom: 20px; }
.btn-cta {
  display: inline-block; background: #667eea; color: white; text-decoration: none; border-radius: 8px;
  padding: 10px 22px; font-size: 14px; font-weight: 600; transition: background .15s;
}
.btn-cta:hover { background: #5a6fd6; }
</style>
