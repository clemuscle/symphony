<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">Crée un projet depuis un Golden Path approuvé</p>
      </div>
      <button class="btn-primary" @click="showForm = !showForm">
        {{ showForm ? '✕ Fermer' : '+ Nouveau projet' }}
      </button>
    </div>

    <!-- Formulaire création -->
    <Transition name="fade">
      <div class="form-card" v-if="showForm">
        <h3>🚀 Nouveau projet</h3>

        <!-- Sélection Golden Path -->
        <div class="gp-section">
          <div class="section-label">Choisis un Golden Path</div>
          <div v-if="loadingGP" class="state-sm">Chargement des templates...</div>
          <div class="gp-grid" v-else>
            <div v-for="gp in goldenPaths" :key="gp.metadata.name"
              class="gp-card" :class="{ selected: selectedGP?.metadata.name === gp.metadata.name }"
              @click="selectGP(gp)">
              <div class="gp-icon">{{ langIcon(gp.spec.language) }}</div>
              <div class="gp-name">{{ gp.metadata.name }}</div>
              <div class="gp-tags">
                <span class="gp-tag">{{ gp.spec.language }}</span>
                <span class="gp-tag">{{ gp.spec.type }}</span>
              </div>
            </div>
            <div class="gp-empty" v-if="!goldenPaths.length">
              Aucun golden path trouvé dans symphony-templates
            </div>
          </div>
        </div>

        <!-- Détails du projet -->
        <Transition name="fade">
          <div class="project-details" v-if="selectedGP">
            <div class="section-label">Détails du projet</div>
            <div class="form-grid">
              <div class="field">
                <label>Nom *</label>
                <input v-model="form.name" placeholder="mon-api" @input="validateName" />
                <span class="field-hint" v-if="nameError">{{ nameError }}</span>
              </div>
              <div class="field">
                <label>Description</label>
                <input v-model="form.description" placeholder="Description courte" />
              </div>
              <div class="field">
                <label>Namespace GitLab</label>
                <input v-model="form.namespace" placeholder="laisser vide = racine" />
              </div>
              <div class="field">
                <label>Port</label>
                <input v-model.number="form.port" type="number" placeholder="8080" />
              </div>
            </div>

            <!-- Résumé -->
            <div class="summary">
              <div class="summary-title">Ce que Symphony va créer :</div>
              <div class="summary-items">
                <div class="summary-item">✅ Repo GitLab <code>{{ form.namespace || 'root' }}/{{ form.name || '...' }}</code></div>
                <div class="summary-item">✅ Pipeline CI/CD</div>
              </div>
            </div>

            <div class="form-footer">
              <button class="btn-primary" :disabled="creating || !form.name || !!nameError"
                @click="createProject">
                {{ creating ? '⏳ Création en cours...' : '🚀 Créer le projet' }}
              </button>
            </div>
          </div>
        </Transition>

        <!-- Résultat -->
        <div class="result-card ok" v-if="createResult">
          <div class="result-title">✅ Projet créé avec succès</div>
          <div class="result-row">
            <span>Repo</span>
            <a :href="createResult.repo.web_url" target="_blank">{{ createResult.repo.web_url }} ↗</a>
          </div>
          <div class="result-actions">
            <button class="btn-secondary" @click="triggerPipeline(createResult.repo)">
              ▶ Déclencher le pipeline test
            </button>
          </div>
          <div class="pipeline-status" v-if="pipelineID">
            <span>Pipeline #{{ pipelineID }}</span>
            <span :class="['status-badge', pipelineStatus]">{{ pipelineStatus || 'pending' }}</span>
          </div>
        </div>
        <div class="result-card err" v-if="createError">❌ {{ createError }}</div>
      </div>
    </Transition>

    <!-- Tabs : Projets Symphony vs Tous les repos -->
    <div class="tabs">
      <button :class="['tab', activeTab === 'symphony' ? 'active' : '']"
        @click="activeTab = 'symphony'">
        Projets Symphony ({{ symphonyProjects.length }})
      </button>
      <button :class="['tab', activeTab === 'all' ? 'active' : '']"
        @click="activeTab = 'all'">
        Tous les repos GitLab ({{ repos.length }})
      </button>
      <button class="tab-action" @click="reloadTemplates">↻ Recharger les templates</button>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <!-- Projets Symphony -->
    <div v-if="activeTab === 'symphony'">
      <div v-if="loadingProjects" class="state">Chargement...</div>
      <div class="projects-grid" v-else>
        <div class="project-card" v-for="p in symphonyProjects" :key="p.id">
          <div class="project-header">
            <span class="project-name">{{ p.name }}</span>
            <span class="lang-badge">{{ langIcon(p.language) }} {{ p.language }}</span>
          </div>
          <div class="project-desc">{{ p.description || 'Pas de description' }}</div>
          <div class="project-meta">
            <span>📁 {{ p.repo_path }}</span>
            <span>🔌 :{{ p.port }}</span>
          </div>
          <div class="project-footer">
            <a :href="p.repo_url" target="_blank" class="btn-ghost">GitLab ↗</a>
            <button class="btn-ghost" @click="triggerPipelineByPath(p.repo_path)">▶ Pipeline</button>
          </div>
          <div class="pipeline-status" v-if="repoPipelines[p.repo_path]">
            <span>Pipeline #{{ repoPipelines[p.repo_path].id }}</span>
            <span :class="['status-badge', repoPipelines[p.repo_path].status]">
              {{ repoPipelines[p.repo_path].status }}
            </span>
          </div>
        </div>
        <div class="empty" v-if="!symphonyProjects.length">
          Aucun projet créé via Symphony. Clique sur "+ Nouveau projet".
        </div>
      </div>
    </div>

    <!-- Tous les repos GitLab -->
    <div v-if="activeTab === 'all'">
      <div v-if="loadingRepos" class="state">Chargement...</div>
      <div class="projects-grid" v-else>
        <div class="project-card" v-for="repo in repos" :key="repo.path">
          <div class="project-header">
            <span class="project-name">{{ repo.name }}</span>
          </div>
          <div class="project-meta"><span>📁 {{ repo.path }}</span></div>
          <div class="project-footer">
            <a :href="repo.web_url" target="_blank" class="btn-ghost">GitLab ↗</a>
            <button class="btn-ghost" @click="triggerPipelineByPath(repo.path)">▶ Pipeline</button>
          </div>
          <div class="pipeline-status" v-if="repoPipelines[repo.path]">
            <span>Pipeline #{{ repoPipelines[repo.path].id }}</span>
            <span :class="['status-badge', repoPipelines[repo.path].status]">
              {{ repoPipelines[repo.path].status }}
            </span>
          </div>
        </div>
        <div class="empty" v-if="!repos.length">Aucun repo trouvé</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

const showForm = ref(false)
const activeTab = ref('symphony')
const goldenPaths = ref([])
const selectedGP = ref(null)
const loadingGP = ref(true)
const creating = ref(false)
const createResult = ref(null)
const createError = ref(null)
const symphonyProjects = ref([])
const repos = ref([])
const loadingProjects = ref(true)
const loadingRepos = ref(true)
const pipelineID = ref(null)
const pipelineStatus = ref(null)
const repoPipelines = ref({})
const nameError = ref('')
const error = ref(null)

const form = ref({
  name: '', description: '', namespace: '', port: 8080,
})

const langIcon = (lang) => ({
  go: '🐹', python: '🐍', node: '💚', java: '☕', default: '📦'
})[lang] || '📦'

onMounted(async () => {
  await Promise.all([loadGoldenPaths(), loadProjects(), loadRepos()])
})

function selectGP(gp) {
  selectedGP.value = gp
  form.value.port = 8080
}

function validateName() {
  const val = form.value.name
  if (!val) { nameError.value = ''; return }
  if (!/^[a-z0-9-]+$/.test(val)) {
    nameError.value = 'Uniquement lettres minuscules, chiffres et tirets'
  } else {
    nameError.value = ''
  }
}

async function loadGoldenPaths() {
  loadingGP.value = true
  try {
    const { data } = await api.getGoldenPaths()
    goldenPaths.value = data || []
    error.value = null
  } catch (e) {
    goldenPaths.value = []
    error.value = e.response?.data?.error || e.message
  } finally { loadingGP.value = false }
}

async function loadProjects() {
  loadingProjects.value = true
  try {
    const { data } = await api.listProjects()
    symphonyProjects.value = data || []
    error.value = null
  } catch (e) {
    symphonyProjects.value = []
    error.value = e.response?.data?.error || e.message
  } finally { loadingProjects.value = false }
}

async function loadRepos() {
  loadingRepos.value = true
  try {
    const { data } = await api.listRepos()
    repos.value = data || []
    error.value = null
  } catch (e) {
    repos.value = []
    error.value = e.response?.data?.error || e.message
  } finally { loadingRepos.value = false }
}

async function createProject() {
  creating.value = true
  createResult.value = null
  createError.value = null
  try {
    const payload = {
      ...form.value,
      language: selectedGP.value.spec.language,
      type: selectedGP.value.spec.type,
    }
    const { data } = await api.createProject(payload)
    createResult.value = data
    await loadProjects()
    await loadRepos()
  } catch (e) {
    createError.value = e.response?.data?.error || e.message
  } finally { creating.value = false }
}

async function triggerPipeline(repo) {
  try {
    const { data } = await api.triggerPipeline(repo.path, 'main', {})
    pipelineID.value = data.pipeline_id
    pipelineStatus.value = 'pending'
    pollStatus(repo.path, data.pipeline_id)
  } catch (e) {
    alert('Erreur: ' + (e.response?.data?.error || e.message))
  }
}

async function triggerPipelineByPath(path) {
  try {
    const { data } = await api.triggerPipeline(path, 'main', {})
    repoPipelines.value[path] = { id: data.pipeline_id, status: 'pending' }
    pollStatusForRepo(path, data.pipeline_id)
  } catch (e) {
    alert('Erreur: ' + (e.response?.data?.error || e.message))
  }
}

async function reloadTemplates() {
  try {
    const { data } = await api.reloadTemplates()
    alert(`✅ ${data.golden_paths} golden path(s) rechargé(s)`)
    await loadGoldenPaths()
  } catch (e) {
    alert('Erreur: ' + e.message)
  }
}

function pollStatus(path, id, attempts = 0) {
  if (attempts > 30) return
  setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(path, id)
      pipelineStatus.value = data.status
      if (!['success', 'failed', 'canceled'].includes(data.status))
        pollStatus(path, id, attempts + 1)
    } catch {}
  }, 5000)
}

function pollStatusForRepo(path, id, attempts = 0) {
  if (attempts > 30) return
  setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(path, id)
      repoPipelines.value[path] = { id, status: data.status }
      if (!['success', 'failed', 'canceled'].includes(data.status))
        pollStatusForRepo(path, id, attempts + 1)
    } catch {}
  }, 5000)
}
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.btn-secondary { padding: 8px 16px; background: white; border: 1px solid #667eea; color: #667eea; border-radius: 8px; cursor: pointer; font-size: 13px; }
.btn-ghost { padding: 6px 12px; background: transparent; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #555; text-decoration: none; display: inline-block; }
.btn-ghost:hover { background: #f5f5f5; }
.form-card { background: white; border: 1px solid #e2e2e2; border-radius: 14px; padding: 24px; margin-bottom: 24px; }
h3 { font-size: 17px; font-weight: 700; margin-bottom: 20px; }
.gp-section { margin-bottom: 24px; }
.section-label { font-size: 13px; font-weight: 600; color: #555; text-transform: uppercase; letter-spacing: .05em; margin-bottom: 12px; }
.state-sm { color: #888; font-size: 13px; padding: 12px 0; }
.gp-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.gp-card { border: 2px solid #e2e2e2; border-radius: 12px; padding: 16px; cursor: pointer; transition: all .15s; background: white; }
.gp-card:hover { border-color: #667eea; transform: translateY(-2px); box-shadow: 0 4px 12px #667eea15; }
.gp-card.selected { border-color: #667eea; background: #f0f4ff; box-shadow: 0 0 0 3px #667eea22; }
.gp-icon { font-size: 28px; margin-bottom: 8px; }
.gp-name { font-weight: 600; font-size: 14px; margin-bottom: 4px; }
.gp-desc { font-size: 12px; color: #777; margin-bottom: 10px; line-height: 1.4; }
.gp-tags { display: flex; gap: 6px; flex-wrap: wrap; }
.gp-tag { font-size: 11px; background: #eef2ff; color: #667eea; padding: 2px 8px; border-radius: 20px; }
.gp-empty { color: #888; font-size: 13px; padding: 20px; grid-column: 1/-1; text-align: center; }
.project-details { border-top: 1px solid #f0f0f0; padding-top: 20px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 20px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field label { font-size: 13px; font-weight: 500; color: #555; }
.field input { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; }
.field input:focus { outline: none; border-color: #667eea; }
.field-hint { font-size: 12px; color: #e53e3e; }
.summary { background: #f8f9fb; border-radius: 10px; padding: 14px 16px; margin-bottom: 16px; }
.summary-title { font-size: 13px; font-weight: 600; color: #555; margin-bottom: 10px; }
.summary-items { display: flex; flex-direction: column; gap: 6px; }
.summary-item { font-size: 13px; color: #444; }
.summary-item code { font-size: 12px; background: #eef2ff; color: #667eea; padding: 1px 6px; border-radius: 4px; }
.form-footer { display: flex; justify-content: flex-end; }
.result-card { padding: 16px; border-radius: 10px; margin-top: 16px; }
.result-card.ok { background: #f0fff4; border: 1px solid #9ae6b4; }
.result-card.err { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; }
.result-title { font-weight: 700; color: #276749; margin-bottom: 10px; }
.result-row { display: flex; gap: 12px; margin-bottom: 6px; font-size: 14px; align-items: center; }
.result-row span { color: #666; min-width: 70px; font-size: 13px; }
.result-row a { color: #667eea; text-decoration: none; }
.result-row code { font-size: 12px; background: #e8f5e9; padding: 2px 6px; border-radius: 4px; }
.result-actions { margin-top: 12px; }
.pipeline-status { display: flex; align-items: center; gap: 10px; margin-top: 10px; font-size: 13px; color: #666; padding: 8px 12px; background: #f8f9fb; border-radius: 8px; }
.status-badge { padding: 2px 10px; border-radius: 20px; font-size: 12px; font-weight: 500; }
.status-badge.success { background: #f0fff4; color: #276749; }
.status-badge.failed { background: #fff5f5; color: #c53030; }
.status-badge.running { background: #ebf8ff; color: #2b6cb0; }
.status-badge.pending { background: #fffbeb; color: #92400e; }
.tabs { display: flex; align-items: center; gap: 4px; margin-bottom: 16px; border-bottom: 1px solid #e2e2e2; padding-bottom: 0; }
.tab { padding: 10px 16px; background: none; border: none; cursor: pointer; font-size: 14px; color: #888; border-bottom: 2px solid transparent; margin-bottom: -1px; }
.tab.active { color: #667eea; border-bottom-color: #667eea; font-weight: 600; }
.tab-action { margin-left: auto; padding: 6px 12px; background: white; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #666; }
.state { color: #888; padding: 40px; text-align: center; }
.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }
.projects-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.project-card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; }
.project-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; }
.project-name { font-weight: 600; font-size: 15px; }
.lang-badge { font-size: 12px; background: #f4f4f4; padding: 2px 8px; border-radius: 20px; color: #555; }
.project-desc { font-size: 13px; color: #777; margin-bottom: 10px; }
.project-meta { display: flex; gap: 12px; font-size: 12px; color: #999; margin-bottom: 12px; }
.project-footer { display: flex; gap: 8px; }
.empty { color: #888; text-align: center; padding: 40px; grid-column: 1/-1; font-size: 14px; }
.fade-enter-active, .fade-leave-active { transition: opacity .2s, transform .2s; }
.fade-enter-from, .fade-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
