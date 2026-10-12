<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">Crée un projet depuis un Golden Path approuvé</p>
      </div>
      <button class="btn-primary" v-if="can('developer')" @click="showForm = !showForm">
        {{ showForm ? '✕ Fermer' : '+ Nouveau projet' }}
      </button>
    </div>

    <!-- Formulaire création -->
    <Transition name="fade">
      <div class="form-card" v-if="showForm">
        <h3>🚀 Nouveau projet</h3>

        <!-- Sélection Golden Path -->
        <div class="gp-section">
          <div class="section-head">
            <div class="section-label">Choisis un Golden Path</div>
            <button class="btn-ghost" v-if="can('admin')" :disabled="syncingGP" @click="syncGoldenPaths">
              {{ syncingGP ? 'Rechargement...' : '↻ Recharger depuis Git' }}
            </button>
          </div>
          <div v-if="loadingGP" class="state-sm">Chargement des templates...</div>
          <div class="gp-grid" v-else>
            <div v-for="gp in goldenPaths" :key="gp.id"
              class="gp-card" :class="{ selected: selectedGP?.id === gp.id }"
              @click="selectGP(gp)">
              <div class="gp-icon">{{ langIcon(gp.language) }}</div>
              <div class="gp-name">{{ gp.name }}</div>
              <div class="gp-desc">{{ gp.description }}</div>
              <div class="gp-tags">
                <span class="gp-tag">{{ gp.language }}</span>
                <span class="gp-tag">{{ gp.type }}</span>
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
                <div class="summary-item">✅ Repo GitLab <code>{{ form.namespace || 'root' }}/{{ form.name || '...' }}</code>, squelette et CI <code>{{ selectedGP.ci_template }}</code> en un commit</div>
                <div class="summary-item">✅ Fiche au catalogue et webhook de pipeline (suivi en direct)</div>
                <div class="summary-item">✅ Première pipeline : tests et image dans le registry</div>
                <div class="summary-item">✅ Runtime de déploiement : <code>{{ selectedGP.runtime_provider || 'par défaut' }}</code></div>
              </div>
            </div>

            <div class="form-footer">
              <button class="btn-primary" :disabled="creating || !form.name || !!nameError"
                @click="createProject">
                {{ creating ? '⏳ Envoi...' : '🚀 Créer le projet' }}
              </button>
            </div>
          </div>
        </Transition>

        <!-- Avancement de la création, en direct (tâche asynchrone) -->
        <div class="task-wrap" v-if="createTask">
          <TaskProgress :task="createTask" />
          <div class="next" v-if="createTask.status === 'SUCCESS'">
            Image construite :
            <RouterLink :to="{ path: '/deployments', query: { project: createTask.resource } }">déployer {{ createTask.resource }} →</RouterLink>
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
    </div>

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
          <div class="project-task" v-if="activeTaskFor(p.name)">
            ⚡ {{ kindLabels[activeTaskFor(p.name).kind] }} en cours :
            {{ activeTaskFor(p.name).steps[activeTaskFor(p.name).step] }}
            <span v-if="activeTaskFor(p.name).detail">— {{ activeTaskFor(p.name).detail }}</span>
          </div>
          <div class="project-footer">
            <a :href="p.repo_url" target="_blank" class="btn-ghost">GitLab ↗</a>
            <button class="btn-ghost" v-if="can('developer')" @click="triggerPipelineByPath(p.repo_path)">▶ Pipeline</button>
            <button class="btn-ghost btn-danger" v-if="can('admin')" :disabled="isDeleting(p)" @click="deleteProject(p)">
              {{ isDeleting(p) ? 'Suppression…' : 'Supprimer' }}
            </button>
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
            <button class="btn-ghost" v-if="can('developer')" @click="triggerPipelineByPath(repo.path)">▶ Pipeline</button>
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
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api'
import { can } from '../stores/session'
import TaskProgress from '../components/TaskProgress.vue'
import { taskStore, trackTask, activeTasks, activeTaskFor, kindLabels } from '../stores/tasks'

const showForm = ref(false)
const activeTab = ref('symphony')
const goldenPaths = ref([])
const selectedGP = ref(null)
const loadingGP = ref(true)
const syncingGP = ref(false)
const creating = ref(false)
const createTaskId = ref(null)
const createTask = computed(() => taskStore.byId[createTaskId.value])
const createError = ref(null)
const symphonyProjects = ref([])
const repos = ref([])
const loadingProjects = ref(true)
const loadingRepos = ref(true)
const repoPipelines = ref({})
const nameError = ref('')

const form = ref({
  name: '', description: '', namespace: '', port: 8080,
})

const langIcon = (lang) => ({
  go: '🐹', python: '🐍', node: '💚', java: '☕', default: '📦'
})[lang] || '📦'

// Le projet apparaît en base à la 2e étape de sa création, et disparaît à
// la fin de sa suppression : la liste est relue quand une tâche avance
watch(() => activeTasks.value.map(t => `${t.id}:${t.step}`).join(), (now, before) => {
  if (before !== undefined) { loadProjects(); loadRepos() }
})
watch(() => createTask.value?.status, (status) => {
  if (status === 'SUCCESS' || status === 'FAILED') { loadProjects(); loadRepos() }
})

const isDeleting = (p) => p.deleting || !!activeTaskFor(p.name, 'delete_project')

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
  } catch { goldenPaths.value = [] }
  finally { loadingGP.value = false }
}

// Les golden paths sont rechargés depuis Git toutes les 15 s ; ce bouton
// force le rechargement (asynchrone côté serveur) puis relit la liste
async function syncGoldenPaths() {
  syncingGP.value = true
  try {
    await api.sync()
    await new Promise(r => setTimeout(r, 1500))
    await loadGoldenPaths()
  } finally { syncingGP.value = false }
}

async function loadProjects() {
  if (!symphonyProjects.value.length) loadingProjects.value = true
  try {
    const { data } = await api.listProjects()
    symphonyProjects.value = data || []
  } catch { symphonyProjects.value = [] }
  finally { loadingProjects.value = false }
}

async function loadRepos() {
  loadingRepos.value = true
  try {
    const { data } = await api.listRepos()
    repos.value = data || []
  } catch { repos.value = [] }
  finally { loadingRepos.value = false }
}

// La création est une tâche : l'API répond tout de suite (202) et la
// progression arrive en direct par le flux des tâches
async function createProject() {
  creating.value = true
  createTaskId.value = null
  createError.value = null
  try {
    const payload = {
      ...form.value,
      golden_path_id: selectedGP.value.id,
      scm_provider: selectedGP.value.scm_provider,
      runtime_provider: selectedGP.value.runtime_provider,
    }
    const { data } = await api.createProject(payload)
    createTaskId.value = trackTask(data)
  } catch (e) {
    createError.value = e.response?.data?.error || e.message
  } finally { creating.value = false }
}

async function deleteProject(p) {
  if (!confirm(`Supprimer définitivement ${p.name} ?\n\nLe repo ${p.repo_path}, ses images et sa fiche catalogue seront supprimés.`)) return
  try {
    const { data } = await api.deleteProject(p.name)
    trackTask(data)
  } catch (e) {
    alert('Erreur: ' + (e.response?.data?.error || e.message))
  }
}

async function triggerPipelineByPath(path) {
  try {
    const { data } = await api.triggerPipeline(path, 'main', {})
    repoPipelines.value[path] = { id: data.id, status: 'pending' }
    pollStatusForRepo(path, data.id)
  } catch (e) {
    alert('Erreur: ' + (e.response?.data?.error || e.message))
  }
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
.btn-danger { color: #c53030; border-color: #feb2b2; margin-left: auto; }
.btn-danger:hover { background: #fff5f5; }
.btn-danger:disabled { opacity: .6; cursor: wait; }
.task-wrap { margin-top: 20px; }
.next { margin-top: 10px; font-size: 14px; }
.next a { color: #667eea; text-decoration: none; font-weight: 500; }
.project-task { font-size: 12px; color: #4c51bf; background: #ebf4ff; border-radius: 6px; padding: 6px 10px; margin-bottom: 10px; }
.form-card { background: white; border: 1px solid #e2e2e2; border-radius: 14px; padding: 24px; margin-bottom: 24px; }
h3 { font-size: 17px; font-weight: 700; margin-bottom: 20px; }
.gp-section { margin-bottom: 24px; }
.section-head { display: flex; justify-content: space-between; align-items: baseline; }
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
.state { color: #888; padding: 40px; text-align: center; }
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
