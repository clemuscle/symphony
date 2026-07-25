<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Déploiements</h2>
        <p class="subtitle">Containers Docker actifs sur cet environnement</p>
      </div>
      <div class="header-actions">
        <div class="env-filter">
          <button
            v-for="opt in envFilters"
            :key="opt.value"
            :class="['env-filter-btn', { active: envFilter === opt.value }]"
            @click="envFilter = opt.value"
          >{{ opt.label }}</button>
        </div>
        <button class="btn-refresh" @click="load(true)">↻ Rafraîchir</button>
      </div>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <div v-if="loading" class="state">Chargement...</div>

    <div class="grid" v-else>
      <div class="deploy-card" v-for="d in filteredDeployments" :key="d.id">
        <div class="deploy-header">
          <span class="deploy-name">{{ d.project_name || d.id }}</span>
          <span :class="['status', d.status]">{{ d.status }}</span>
        </div>
        <div class="deploy-meta">
          <span :class="['env-badge', d.environment]">{{ d.environment }}</span>
          <span class="tag-chip" v-for="t in d.tags" :key="t">{{ t }}</span>
        </div>
        <div class="deploy-image">🐳 {{ d.image }}</div>
        <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} ↗</a>
        <div class="deploy-footer" v-if="canDeploy">
          <button class="btn-stop" @click="stop(d.id)">⏹ Stop</button>
        </div>
      </div>
      <div class="empty" v-if="!filteredDeployments.length">
        <div class="empty-icon">🐳</div>
        <div>Aucun container actif</div>
        <div class="empty-sub">Crée et déploie un projet depuis l'onglet Projets</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const { canDeploy } = useAuth()

const deployments = ref([])
const loading = ref(true)
const error = ref(null)
let pollInterval = null

const envFilters = [
  { value: 'all', label: 'Tous' },
  { value: 'prod', label: 'Prod' },
  { value: 'preprod', label: 'Préprod' },
]
const envFilter = ref('all')
const filteredDeployments = computed(() =>
  envFilter.value === 'all' ? deployments.value : deployments.value.filter(d => d.environment === envFilter.value)
)

onMounted(() => {
  load(true)
  pollInterval = setInterval(load, 7000)
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})

async function load(isInitial = false) {
  if (isInitial) loading.value = true
  try {
    const { data } = await api.listDeployments()
    deployments.value = data || []
    error.value = null
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally { loading.value = false }
}

async function stop(id) {
  if (!confirm(`Stopper le container ${id} ?`)) return
  try { await api.stopDeployment(id); await load(true) }
  catch (e) { alert(e.response?.data?.error || e.message) }
}
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.header-actions { display: flex; align-items: center; gap: 10px; }
.env-filter { display: flex; gap: 2px; background: #f0f2f5; border-radius: 8px; padding: 3px; }
.env-filter-btn { padding: 6px 14px; background: transparent; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; color: #666; transition: all .15s; }
.env-filter-btn:hover { color: #333; }
.env-filter-btn.active { background: white; color: #1a1a1a; font-weight: 600; box-shadow: 0 1px 2px rgba(0,0,0,.08); }
.btn-refresh { padding: 8px 16px; background: white; border: 1px solid #ddd; border-radius: 8px; cursor: pointer; font-size: 14px; }
.state { color: #888; padding: 60px; text-align: center; }
.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.deploy-card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; }
.deploy-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.deploy-name { font-weight: 600; font-size: 15px; }
.deploy-meta { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; flex-wrap: wrap; }
.env-badge { font-size: 11px; font-weight: 700; padding: 2px 9px; border-radius: 20px; text-transform: uppercase; letter-spacing: .03em; }
.env-badge.prod { background: #fef2f2; color: #b91c1c; }
.env-badge.preprod { background: #fffbeb; color: #92400e; }
.env-badge.recette { background: #eff6ff; color: #1d4ed8; }
.tag-chip { font-size: 11px; padding: 2px 8px; border-radius: 10px; background: #f4f4f7; color: #666; font-family: monospace; }
.status { font-size: 12px; padding: 3px 10px; border-radius: 20px; font-weight: 500; }
.status.running { background: #f0fff4; color: #276749; }
.status.pending { background: #eff6ff; color: #1d4ed8; }
.status.stopped { background: #f4f4f4; color: #888; }
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
