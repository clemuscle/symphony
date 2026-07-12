<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Journal d'audit</h2>
        <p class="subtitle">Toutes les actions effectuées sur la plateforme</p>
      </div>
      <button class="btn-refresh" @click="load" :disabled="loading">↻ Rafraîchir</button>
    </div>

    <!-- Filtres -->
    <div class="filter-bar">
      <input
        v-model="search"
        class="filter-input"
        placeholder="Rechercher dans les ressources ou détails…"
      />
      <select v-model="filterActor" class="filter-select">
        <option value="">Tous les acteurs</option>
        <option v-for="a in actors" :key="a" :value="a">{{ a }}</option>
      </select>
      <select v-model="filterAction" class="filter-select">
        <option value="">Toutes les actions</option>
        <option v-for="a in actionTypes" :key="a" :value="a">{{ actionLabel(a) }}</option>
      </select>
      <button v-if="hasFilter" class="btn-reset" @click="resetFilters">✕ Réinitialiser</button>
      <span class="count-badge">{{ filtered.length }} entrée{{ filtered.length > 1 ? 's' : '' }}</span>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <div v-if="loading" class="skeleton-rows">
      <div v-for="i in 8" :key="i" class="skeleton-row"></div>
    </div>

    <div v-else-if="!filtered.length" class="empty-state">
      <div class="empty-icon">🔍</div>
      <div>{{ entries.length ? 'Aucune entrée ne correspond aux filtres' : 'Aucune action enregistrée pour l\'instant' }}</div>
    </div>

    <div v-else class="audit-list">
      <div v-for="entry in filtered" :key="entry.id" class="audit-row">
        <div class="row-time" :title="fullDate(entry.created_at)">{{ timeAgo(entry.created_at) }}</div>
        <div class="row-badge" :class="actionClass(entry.action)">{{ actionLabel(entry.action) }}</div>
        <div class="row-actor">
          <span class="actor-chip">{{ shortActor(entry.user_id) }}</span>
        </div>
        <div class="row-resource">{{ entry.resource }}</div>
        <div class="row-details">{{ entry.details }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

const entries = ref([])
const loading = ref(true)
const error = ref(null)
const search = ref('')
const filterActor = ref('')
const filterAction = ref('')

const ACTION_META = {
  create_project:          { label: 'Création projet',     cls: 'act-create' },
  deploy:                  { label: 'Déploiement',         cls: 'act-deploy' },
  create_recette:          { label: 'Recette créée',       cls: 'act-recette' },
  destroy_recette:         { label: 'Recette détruite',    cls: 'act-destroy' },
  stop_deployment:         { label: 'Dépl. arrêté',        cls: 'act-destroy' },
  trigger_pipeline:        { label: 'Pipeline déclenché',  cls: 'act-pipeline' },
  trigger_action:          { label: 'Action déclenchée',   cls: 'act-pipeline' },
  update_pipeline_status:  { label: 'Statut pipeline',     cls: 'act-status' },
  provisioning_step_failed:{ label: 'Étape échouée',       cls: 'act-error' },
}

const actionLabel = (a) => ACTION_META[a]?.label ?? a
const actionClass = (a) => ACTION_META[a]?.cls ?? 'act-default'

const actors = computed(() => [...new Set(entries.value.map(e => e.user_id).filter(Boolean))].sort())
const actionTypes = computed(() => [...new Set(entries.value.map(e => e.action))].sort())
const hasFilter = computed(() => search.value || filterActor.value || filterAction.value)

const filtered = computed(() => {
  let list = entries.value
  if (filterActor.value) list = list.filter(e => e.user_id === filterActor.value)
  if (filterAction.value) list = list.filter(e => e.action === filterAction.value)
  if (search.value) {
    const q = search.value.toLowerCase()
    list = list.filter(e =>
      e.resource?.toLowerCase().includes(q) ||
      e.details?.toLowerCase().includes(q) ||
      e.user_id?.toLowerCase().includes(q)
    )
  }
  return list
})

function resetFilters() {
  search.value = ''
  filterActor.value = ''
  filterAction.value = ''
}

function shortActor(uid) {
  if (!uid) return '—'
  return uid.split('@')[0] || uid
}

function timeAgo(ts) {
  const diff = (Date.now() - new Date(ts)) / 1000
  if (diff < 60) return 'à l\'instant'
  if (diff < 3600) return `il y a ${Math.floor(diff / 60)} min`
  if (diff < 86400) return `il y a ${Math.floor(diff / 3600)} h`
  if (diff < 86400 * 7) return `il y a ${Math.floor(diff / 86400)} j`
  return fullDate(ts)
}

function fullDate(ts) {
  return new Date(ts).toLocaleString('fr-FR', {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit'
  })
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const { data } = await api.listAudit()
    entries.value = data
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.btn-refresh { background: white; border: 1px solid #ddd; border-radius: 6px; padding: 7px 14px; font-size: 13px; cursor: pointer; }
.btn-refresh:hover { border-color: #667eea; color: #667eea; }

.filter-bar { display: flex; gap: 10px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
.filter-input { flex: 1; min-width: 220px; border: 1px solid #ddd; border-radius: 8px; padding: 8px 12px; font-size: 13px; outline: none; }
.filter-input:focus { border-color: #667eea; }
.filter-select { border: 1px solid #ddd; border-radius: 8px; padding: 8px 12px; font-size: 13px; background: white; cursor: pointer; outline: none; }
.filter-select:focus { border-color: #667eea; }
.btn-reset { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 7px 12px; font-size: 12px; cursor: pointer; }
.count-badge { font-size: 12px; color: #888; white-space: nowrap; margin-left: auto; }

.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.skeleton-rows { display: flex; flex-direction: column; gap: 6px; }
.skeleton-row { height: 44px; border-radius: 8px; background: linear-gradient(90deg,#f0f2f5 25%,#e8eaf0 50%,#f0f2f5 75%); background-size: 200% 100%; animation: shimmer 1.4s infinite; }
@keyframes shimmer { 0%{background-position:200% 0} 100%{background-position:-200% 0} }

.empty-state { text-align: center; padding: 60px 0; color: #aaa; }
.empty-icon { font-size: 36px; margin-bottom: 12px; }

.audit-list { background: white; border: 1px solid #e5e7eb; border-radius: 12px; overflow: hidden; }

.audit-row {
  display: grid;
  grid-template-columns: 130px 160px 130px 1fr 1fr;
  gap: 12px;
  align-items: center;
  padding: 10px 16px;
  border-bottom: 1px solid #f5f5f5;
  font-size: 13px;
  transition: background .1s;
}
.audit-row:last-child { border-bottom: none; }
.audit-row:hover { background: #fafbfc; }

.row-time { color: #999; font-size: 12px; white-space: nowrap; }
.row-resource { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row-details { color: #666; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: monospace; }

.actor-chip {
  background: #f0f2f5;
  border-radius: 20px;
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 500;
  color: #444;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 120px;
  display: inline-block;
}

/* Action badges */
.row-badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 10px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}
.act-create   { background: #d1fae5; color: #065f46; }
.act-deploy   { background: #dbeafe; color: #1e40af; }
.act-recette  { background: #ede9fe; color: #5b21b6; }
.act-destroy  { background: #fee2e2; color: #991b1b; }
.act-pipeline { background: #e0f2fe; color: #0369a1; }
.act-status   { background: #f3f4f6; color: #374151; }
.act-error    { background: #fef3c7; color: #92400e; }
.act-default  { background: #f3f4f6; color: #6b7280; }
</style>
