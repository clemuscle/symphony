<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Déploiements</h2>
        <p class="subtitle">Containers Docker actifs sur cet environnement</p>
      </div>
      <button class="btn-refresh" @click="load">↻ Rafraîchir</button>
    </div>

    <div v-if="loading" class="state">Chargement...</div>

    <div class="grid" v-else>
      <div class="deploy-card" v-for="d in deployments" :key="d.ID">
        <div class="deploy-header">
          <span class="deploy-name">{{ d.ProjectName || d.ID }}</span>
          <span :class="['status', d.Status]">{{ d.Status }}</span>
        </div>
        <div class="deploy-image">🐳 {{ d.Image }}</div>
        <a v-if="d.URL" :href="d.URL" target="_blank" class="deploy-url">{{ d.URL }} ↗</a>
        <div class="deploy-footer">
          <button class="btn-stop" @click="stop(d.ID)">⏹ Stop</button>
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

onMounted(load)

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
</style>
