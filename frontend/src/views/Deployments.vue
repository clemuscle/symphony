<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Déploiements</h2>
        <p class="subtitle">Containers Docker actifs sur cet environnement</p>
      </div>
      <button class="btn-refresh" @click="load">↻ Rafraîchir</button>
    </div>

    <!-- Déploiement manuel -->
    <form class="form-card" @submit.prevent="deploy">
      <h3>🚀 Nouveau déploiement</h3>
      <div class="form-grid">
        <div class="field">
          <label>Projet *</label>
          <input v-model="form.project_name" placeholder="mon-api" required />
        </div>
        <div class="field">
          <label>Image *</label>
          <input v-model="form.image" placeholder="gitlab.local:5050/root/mon-api" required />
        </div>
        <div class="field">
          <label>Tag</label>
          <input v-model="form.tag" placeholder="latest" />
        </div>
        <div class="field">
          <label>Port</label>
          <input v-model.number="form.port" type="number" placeholder="8080" />
        </div>
        <div class="field">
          <label>Runtime provider</label>
          <input v-model="form.runtime_provider" placeholder="docker-local" />
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
      <div class="deploy-card" v-for="d in deployments" :key="d.id">
        <div class="deploy-header">
          <span class="deploy-name">{{ d.name || d.id }}</span>
          <span :class="['status', d.status]">{{ d.status }}</span>
        </div>
        <div class="deploy-image">🐳 {{ d.image }}</div>
        <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} ↗</a>
        <div class="deploy-footer">
          <button class="btn-stop" @click="stop(d.id)">⏹ Stop</button>
        </div>
      </div>
      <div class="empty" v-if="!deployments.length">
        <div class="empty-icon">🐳</div>
        <div>Aucun container actif</div>
        <div class="empty-sub">Crée et déploie un projet depuis l'onglet Projets</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'

const deployments = ref([])
const loading = ref(true)
const deploying = ref(false)
const deployResult = ref(null)
const deployError = ref(null)

const form = ref({
  project_name: '', image: '', tag: 'latest', port: 8080, runtime_provider: 'docker-local',
})

onMounted(load)

async function deploy() {
  deploying.value = true
  deployResult.value = null
  deployError.value = null
  try {
    const { data } = await api.deploy(form.value)
    deployResult.value = data
    await load()
  } catch (e) {
    deployError.value = e.response?.data?.error || e.message
  } finally { deploying.value = false }
}

async function load() {
  loading.value = true
  try { const { data } = await api.listDeployments(); deployments.value = data || [] }
  catch { deployments.value = [] }
  finally { loading.value = false }
}

async function stop(id) {
  if (!confirm(`Stopper le container ${id} ?`)) return
  try { await api.stopDeployment(id); await load() }
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
.status.paused { background: #fffbeb; color: #92400e; }
.deploy-image { font-size: 13px; color: #666; margin-bottom: 8px; }
.deploy-url { display: block; font-size: 13px; color: #667eea; text-decoration: none; margin-bottom: 12px; }
.deploy-footer { border-top: 1px solid #f0f0f0; padding-top: 12px; }
.btn-stop { padding: 6px 14px; background: #fff5f5; color: #c53030; border: 1px solid #fed7d7; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn-stop:hover { background: #fed7d7; }
.empty { color: #888; text-align: center; padding: 60px; grid-column: 1/-1; }
.empty-icon { font-size: 48px; margin-bottom: 12px; }
.empty-sub { font-size: 13px; margin-top: 6px; }
.form-card { background: white; border: 1px solid #e2e2e2; border-radius: 14px; padding: 24px; margin-bottom: 24px; }
.form-card h3 { font-size: 16px; margin-bottom: 16px; }
.form-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 14px; margin-bottom: 20px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field label { font-size: 13px; font-weight: 500; color: #555; }
.field input { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; }
.field input:focus { outline: none; border-color: #667eea; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.result-card { padding: 14px 16px; border-radius: 10px; margin-top: 16px; font-size: 14px; }
.result-card.ok { background: #f0fff4; border: 1px solid #9ae6b4; }
.result-card.ok a { color: #667eea; text-decoration: none; }
.result-card.err { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; }
</style>
