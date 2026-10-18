<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Déploiements</h2>
        <p class="subtitle">Services déployés, sur tous les runtimes (Docker, Kubernetes via Argo CD)</p>
      </div>
      <button class="btn-refresh" @click="load()"><AppIcon name="refresh" /> Rafraîchir</button>
    </div>

    <!-- Déploiement manuel -->
    <form class="form-card" v-if="can('developer')" @submit.prevent="deploy">
      <h3><AppIcon name="deployments" /> Nouveau déploiement</h3>
      <div class="form-grid">
        <div class="field">
          <label>Projet *</label>
          <input v-model="form.project_name" placeholder="mon-api" list="projects" required />
          <datalist id="projects">
            <option v-for="p in projects.filter(p => canOperate(p.team))" :key="p.name" :value="p.name" />
          </datalist>
        </div>
        <div class="field">
          <label>Image *</label>
          <input v-model="form.image" placeholder="gitlab.local:5050/root/mon-api" required />
        </div>
        <div class="field">
          <label>Tag</label>
          <input v-model="form.tag" placeholder="latest" list="tags" />
          <datalist id="tags">
            <option v-for="t in tags" :key="t" :value="t" />
          </datalist>
          <span class="hint" v-if="imageHint">{{ imageHint }}</span>
        </div>
        <div class="field">
          <label>Port de l'appli</label>
          <input v-model.number="form.port" type="number" placeholder="8080" />
        </div>
        <div class="field">
          <label>Port hôte (docker)</label>
          <input v-model.number="form.host_port" type="number" placeholder="auto" />
        </div>
        <!-- Avec des environnements : la cible est un environnement (son runtime),
             les environnements protégés ne reçoivent que des promotions -->
        <div class="field" v-if="projectEnvs.length">
          <label>Environnement *</label>
          <select v-model="form.environment" required>
            <option v-for="e in projectEnvs" :key="e.name" :value="e.name" :disabled="e.protected">
              {{ e.name }} ({{ e.runtime }}){{ e.protected ? ' — promotion seulement' : '' }}
            </option>
          </select>
        </div>
        <div class="field" v-else>
          <label>Runtime</label>
          <select v-model="form.runtime_provider">
            <option value="">celui du projet</option>
            <option v-for="rt in runtimes" :key="rt" :value="rt">{{ rt }}</option>
          </select>
        </div>
      </div>
      <button class="btn-primary" type="submit"
        :disabled="deploying || !form.project_name || !form.image">
        <AppIcon :name="deploying ? 'pending' : 'deployments'" /> {{ deploying ? 'Envoi...' : 'Déployer' }}
      </button>
      <!-- Avancement du déploiement, en direct (tâche asynchrone) -->
      <div class="task-wrap" v-if="deployTask">
        <TaskProgress :task="deployTask" />
      </div>
      <div class="result-card err" v-if="deployError"><AppIcon name="error" /> {{ deployError }}</div>
    </form>

    <div v-if="loading" class="state">Chargement...</div>

    <div class="grid" v-else>
      <div class="deploy-card" v-for="d in deployments" :key="d.provider + '/' + d.id">
        <div class="deploy-header">
          <span class="deploy-name">
            {{ d.project || d.name || d.id }}
            <span class="env-badge" v-if="d.environment">{{ d.environment }}</span>
          </span>
          <span :class="['status', d.status]">{{ d.status }}</span>
        </div>
        <div class="deploy-runtime">
          <span class="runtime"><AppIcon :name="d.sync ? 'kubernetes' : runtimeKind(d.provider)" :size="13" /> {{ d.provider }}</span>
          <!-- Runtime GitOps : synchronisation avec Git et santé vues par Argo CD -->
          <span v-if="d.sync" :class="['gitops', d.sync]" title="Synchronisation avec Git (Argo CD)">{{ d.sync }}</span>
          <span v-if="d.health" :class="['gitops', d.health]" title="Santé (Argo CD)">{{ d.health }}</span>
        </div>
        <div class="deploy-image">{{ d.image || '—' }}</div>
        <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} <AppIcon name="external" :size="13" /></a>
        <div class="deploy-footer" v-if="canOperate(teamOf(d.project || d.name))">
          <button class="btn-stop" v-if="isUp(d)" @click="stop(d)"><AppIcon name="stop" :size="13" /> Stop</button>
          <button class="btn-restart" @click="restart(d)"><AppIcon name="restart" :size="13" /> {{ isUp(d) ? 'Redémarrer' : 'Relancer' }}</button>
        </div>
      </div>
      <div class="empty" v-if="!deployments.length">
        <div class="empty-icon"><AppIcon name="deployments" :size="36" /></div>
        <div>Aucun déploiement</div>
        <div class="empty-sub">Crée et déploie un projet depuis l'onglet Projets</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, errorMessage } from '../api'
import AppIcon from '../components/AppIcon.vue'
import TaskProgress from '../components/TaskProgress.vue'
import { runtimeKind } from '../lib/icons'
import { can, canOperate } from '../stores/session'
import { taskStore, trackTask } from '../stores/tasks'
import type { Deployment, Project } from '../types'

const deployments = ref<Deployment[]>([])
const loading = ref(true)
const deploying = ref(false)
const deployTaskId = ref<number | null>(null)
const deployTask = computed(() => (deployTaskId.value ? taskStore.byId[deployTaskId.value] : undefined))
const deployError = ref<string | null>(null)

const form = ref({
  project_name: '', environment: '', image: '', tag: 'latest',
  port: 8080 as number | '', host_port: null as number | '' | null, runtime_provider: '',
})
const runtimes = ref<string[]>([])

const projects = ref<Project[]>([])
// Équipe du projet d'un déploiement (le déploiement porte le nom du projet)
const teamOf = (name: string) => projects.value.find(p => p.name === name)?.team
// Environnements du projet saisi (avec son runtime dans chacun)
const projectEnvs = computed(() => projects.value.find(p => p.name === form.value.project_name)?.environments ?? [])
// Environnement par défaut : le premier qui accepte un déploiement direct
watch(projectEnvs, (envs) => {
  if (!envs.some(e => e.name === form.value.environment && !e.protected)) {
    form.value.environment = envs.find(e => !e.protected)?.name ?? ''
  }
})
const tags = ref<string[]>([])
const imageHint = ref('')
let autoImage = '' // dernière image préremplie : écrasée seulement si l'utilisateur ne l'a pas modifiée

// Chaque étape du déploiement change la liste : on la relit
watch(() => deployTask.value && `${deployTask.value.step}:${deployTask.value.status}`, (v) => { if (v) load(true) })

// Arrivée depuis la création d'un projet : /deployments?project=<nom>
const route = useRoute()

onMounted(() => {
  load()
  // Le projet de ?project= est saisi une fois la liste connue : c'est elle qui
  // déclenche le préremplissage de l'image, des tags et du runtime
  api.listProjects().then(({ data }) => {
    projects.value = data || []
    if (typeof route.query.project === 'string') form.value.project_name = route.query.project
  }).catch(() => {})
  api.getProviders().then(({ data }) => { runtimes.value = data.runtime || [] }).catch(() => {})
})

// Tant qu'un déploiement est en cours (Argo CD synchronise, pods qui
// démarrent), la liste se rafraîchit seule
let timer: ReturnType<typeof setTimeout> | undefined
watch(deployments, (list) => {
  clearTimeout(timer)
  if (list.some(d => d.status === 'deploying')) timer = setTimeout(() => load(true), 3000)
})
onUnmounted(() => clearTimeout(timer))

const isUp = (d: Deployment) => ['running', 'deploying'].includes(d.status)

// Projet connu : préremplir image, tags, port et runtime depuis son registry
watch(() => form.value.project_name, async (name) => {
  tags.value = []
  imageHint.value = ''
  if (!projects.value.some(p => p.name === name)) return
  try {
    const { data } = await api.getProjectImage(name)
    if (form.value.project_name !== name) return
    if (!form.value.image || form.value.image === autoImage) form.value.image = data.image
    autoImage = data.image
    tags.value = data.tags
    // Par défaut, le dernier tag immuable (sha) : latest bouge, et une version
    // en latest ne peut pas être promue
    const pinned = data.tags.find(t => t !== 'latest')
    if (pinned && (form.value.tag === 'latest' || !data.tags.includes(form.value.tag))) form.value.tag = pinned
    else if (data.tags.length && !data.tags.includes(form.value.tag)) form.value.tag = data.tags[0]
    if (data.port) form.value.port = data.port
    if (data.runtime_provider) form.value.runtime_provider = data.runtime_provider
    if (!data.tags.length) imageHint.value = 'Aucune image publiée pour ce projet'
  } catch (e) {
    imageHint.value = errorMessage(e)
  }
})

async function deploy() {
  deploying.value = true
  deployTaskId.value = null
  deployError.value = null
  try {
    // Un champ number vidé vaut "" : l'API attend un entier (0 = défaut).
    // Réponse 202 : le déploiement continue en tâche, suivie en direct.
    const { data } = await api.deploy({
      ...form.value, port: form.value.port || 0, host_port: form.value.host_port || 0,
    })
    deployTaskId.value = trackTask(data)
  } catch (e) {
    deployError.value = errorMessage(e)
  } finally { deploying.value = false }
}

// quiet : rafraîchissement automatique, sans repasser par « Chargement... »
async function load(quiet = false) {
  if (!quiet) loading.value = true
  try { const { data } = await api.listDeployments(); deployments.value = data || [] }
  catch { if (!quiet) deployments.value = [] }
  finally { loading.value = false }
}

async function stop(d: Deployment) {
  if (!confirm(`Arrêter ${d.name || d.id} (${d.provider}) ?`)) return
  try { await api.stopDeployment(d.id, d.provider); await load() }
  catch (e) { alert(errorMessage(e)) }
}

async function restart(d: Deployment) {
  try { await api.restartDeployment(d.id, d.provider); await load() }
  catch (e) { alert(errorMessage(e)) }
}
</script>

<style scoped>
.env-badge { font-size: 11px; font-weight: 500; padding: 1px 8px; border-radius: 20px; background: #ebf4ff; color: #4c51bf; margin-left: 6px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.btn-refresh { padding: 8px 16px; background: white; border: 1px solid #ddd; border-radius: 8px; cursor: pointer; font-size: 14px; }
.state { color: #888; padding: 60px; text-align: center; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.deploy-card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; }
.deploy-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.deploy-name { font-weight: 600; font-size: 15px; }
.status { font-size: 12px; padding: 3px 10px; border-radius: 20px; font-weight: 500; }
.status.running { background: #f0fff4; color: #276749; }
.status.exited { background: #f4f4f4; color: #888; }
.status.paused, .status.deploying { background: #fffbeb; color: #92400e; }
.status.stopped { background: #f4f4f4; color: #888; }
.status.error { background: #fff5f5; color: #c53030; }
.deploy-runtime { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 8px; }
.runtime, .gitops { font-size: 11px; padding: 2px 8px; border-radius: 6px; background: #f4f4f8; color: #555; }
.gitops.Synced, .gitops.Healthy { background: #f0fff4; color: #276749; }
.gitops.OutOfSync, .gitops.Progressing, .gitops.Missing { background: #fffbeb; color: #92400e; }
.gitops.Degraded, .gitops.Unknown { background: #fff5f5; color: #c53030; }
.deploy-image { font-size: 13px; color: #666; margin-bottom: 8px; }
.deploy-url { display: block; font-size: 13px; color: #667eea; text-decoration: none; margin-bottom: 12px; }
.deploy-footer { border-top: 1px solid #f0f0f0; padding-top: 12px; }
.btn-stop { padding: 6px 14px; background: #fff5f5; color: #c53030; border: 1px solid #fed7d7; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-stop:hover { background: #fed7d7; }
.btn-restart { padding: 6px 14px; background: white; color: #555; border: 1px solid #ddd; border-radius: 6px; cursor: pointer; font-size: 13px; margin-left: 6px; }
.btn-restart:hover { background: #f4f4f8; }
.empty { color: #888; text-align: center; padding: 60px; grid-column: 1/-1; }
.empty-icon { color: #bbb; margin-bottom: 12px; }
.empty-sub { font-size: 13px; margin-top: 6px; }
.form-card { background: white; border: 1px solid #e2e2e2; border-radius: 14px; padding: 24px; margin-bottom: 24px; }
.form-card h3 { font-size: 16px; margin-bottom: 16px; }
.form-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 14px; margin-bottom: 20px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field label { font-size: 13px; font-weight: 500; color: #555; }
.field input, .field select { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; }
.field select { background: white; }
.hint { font-size: 12px; color: #888; }
.field input:focus { outline: none; border-color: #667eea; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.task-wrap { margin-top: 16px; }
.result-card { padding: 14px 16px; border-radius: 10px; margin-top: 16px; font-size: 14px; }
.result-card.ok { background: #f0fff4; border: 1px solid #9ae6b4; }
.result-card.ok a { color: #667eea; text-decoration: none; }
.result-card.err { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; }
</style>
