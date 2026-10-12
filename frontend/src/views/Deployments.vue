<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Déploiements</h2>
        <p class="subtitle">Services déployés, sur tous les runtimes (Docker, Kubernetes via Argo CD)</p>
      </div>
      <button class="btn-refresh" @click="load">↻ Rafraîchir</button>
    </div>

    <!-- Déploiement manuel -->
    <form class="form-card" v-if="can('developer')" @submit.prevent="deploy">
      <h3>🚀 Nouveau déploiement</h3>
      <div class="form-grid">
        <div class="field">
          <label>Projet *</label>
          <input v-model="form.project_name" placeholder="mon-api" list="projects" required />
          <datalist id="projects">
            <option v-for="p in projects" :key="p.name" :value="p.name" />
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
        <div class="field">
          <label>Runtime</label>
          <select v-model="form.runtime_provider">
            <option value="">celui du projet</option>
            <option v-for="rt in runtimes" :key="rt" :value="rt">{{ rt }}</option>
          </select>
        </div>
      </div>
      <button class="btn-primary" type="submit"
        :disabled="deploying || !form.project_name || !form.image">
        {{ deploying ? '⏳ Déploiement...' : '🚀 Déployer' }}
      </button>
      <div class="result-card ok" v-if="deployResult">
        ✅ Déployé :
        <a v-if="deployResult.url" :href="deployResult.url" target="_blank">{{ deployResult.url }} ↗</a>
        <code v-else>{{ deployResult.id }}</code>
      </div>
      <div class="result-card err" v-if="deployError">❌ {{ deployError }}</div>
    </form>

    <div v-if="loading" class="state">Chargement...</div>

    <div class="grid" v-else>
      <div class="deploy-card" v-for="d in deployments" :key="d.provider + '/' + d.id">
        <div class="deploy-header">
          <span class="deploy-name">{{ d.name || d.id }}</span>
          <span :class="['status', d.status]">{{ d.status }}</span>
        </div>
        <div class="deploy-runtime">
          <span class="runtime">{{ d.provider }}</span>
          <!-- Runtime GitOps : synchronisation avec Git et santé vues par Argo CD -->
          <span v-if="d.sync" :class="['gitops', d.sync]" title="Synchronisation avec Git (Argo CD)">{{ d.sync }}</span>
          <span v-if="d.health" :class="['gitops', d.health]" title="Santé (Argo CD)">{{ d.health }}</span>
        </div>
        <div class="deploy-image">{{ d.sync ? '☸️' : '🐳' }} {{ d.image || '—' }}</div>
        <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} ↗</a>
        <div class="deploy-footer" v-if="can('developer')">
          <button class="btn-stop" v-if="isUp(d)" @click="stop(d)">⏹ Stop</button>
          <button class="btn-restart" @click="restart(d)">↻ {{ isUp(d) ? 'Redémarrer' : 'Relancer' }}</button>
        </div>
      </div>
      <div class="empty" v-if="!deployments.length">
        <div class="empty-icon">🐳</div>
        <div>Aucun déploiement</div>
        <div class="empty-sub">Crée et déploie un projet depuis l'onglet Projets</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { can } from '../stores/session'

const deployments = ref([])
const loading = ref(true)
const deploying = ref(false)
const deployResult = ref(null)
const deployError = ref(null)

const form = ref({
  project_name: '', image: '', tag: 'latest', port: 8080, host_port: null, runtime_provider: '',
})
const runtimes = ref([])

const projects = ref([])
const tags = ref([])
const imageHint = ref('')
let autoImage = '' // dernière image préremplie : écrasée seulement si l'utilisateur ne l'a pas modifiée

onMounted(() => {
  load()
  api.listProjects().then(({ data }) => { projects.value = data || [] }).catch(() => {})
  api.getProviders().then(({ data }) => { runtimes.value = data.runtime || [] }).catch(() => {})
})

// Tant qu'un déploiement est en cours (Argo CD synchronise, pods qui
// démarrent), la liste se rafraîchit seule
let timer = null
watch(deployments, (list) => {
  clearTimeout(timer)
  if (list.some(d => d.status === 'deploying')) timer = setTimeout(() => load(true), 3000)
})
onUnmounted(() => clearTimeout(timer))

const isUp = (d) => ['running', 'deploying'].includes(d.status)

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
    if (data.tags.length && !data.tags.includes(form.value.tag)) form.value.tag = data.tags[0]
    if (data.port) form.value.port = data.port
    if (data.runtime_provider) form.value.runtime_provider = data.runtime_provider
    if (!data.tags.length) imageHint.value = 'Aucune image publiée pour ce projet'
  } catch (e) {
    imageHint.value = e.response?.data?.error || e.message
  }
})

async function deploy() {
  deploying.value = true
  deployResult.value = null
  deployError.value = null
  try {
    // Un champ number vidé vaut "" : l'API attend un entier (0 = défaut)
    const { data } = await api.deploy({
      ...form.value, port: form.value.port || 0, host_port: form.value.host_port || 0,
    })
    deployResult.value = data
    await load()
  } catch (e) {
    deployError.value = e.response?.data?.error || e.message
  } finally { deploying.value = false }
}

// quiet : rafraîchissement automatique, sans repasser par « Chargement... »
async function load(quiet = false) {
  if (!quiet) loading.value = true
  try { const { data } = await api.listDeployments(); deployments.value = data || [] }
  catch { if (!quiet) deployments.value = [] }
  finally { loading.value = false }
}

async function stop(d) {
  if (!confirm(`Arrêter ${d.name || d.id} (${d.provider}) ?`)) return
  try { await api.stopDeployment(d.id, d.provider); await load() }
  catch (e) { alert(e.response?.data?.error || e.message) }
}

async function restart(d) {
  try { await api.restartDeployment(d.id, d.provider); await load() }
  catch (e) { alert(e.response?.data?.error || e.message) }
}
</script>

<style scoped>
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
.empty-icon { font-size: 48px; margin-bottom: 12px; }
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
.result-card { padding: 14px 16px; border-radius: 10px; margin-top: 16px; font-size: 14px; }
.result-card.ok { background: #f0fff4; border: 1px solid #9ae6b4; }
.result-card.ok a { color: #667eea; text-decoration: none; }
.result-card.err { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; }
</style>
