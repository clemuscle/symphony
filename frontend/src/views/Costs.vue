<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Coûts par projet / équipe</h2>
        <p class="subtitle">Estimation basée sur le temps de run des containers et les minutes CI</p>
      </div>
      <div class="header-controls">
        <button class="btn-nav" @click="prevMonth">‹</button>
        <span class="period-label">{{ periodDisplay }}</span>
        <button class="btn-nav" @click="nextMonth" :disabled="isCurrentMonth">›</button>
        <button class="btn-refresh" @click="load" :disabled="loading">↻</button>
      </div>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <div v-if="loading" class="skeleton-block"></div>

    <template v-else-if="data">
      <!-- Total -->
      <div class="total-banner">
        <div class="total-label">Total estimé — {{ data.period }}</div>
        <div class="total-val">{{ fmt(data.total) }} {{ data.currency }}</div>
        <div class="total-sub" v-if="data.total === 0">
          Tarifs à zéro — configurez <code>config/costs.yaml</code> pour valoriser l'usage
        </div>
      </div>

      <!-- Par projet -->
      <section class="section">
        <h3 class="section-title">📦 Par projet</h3>
        <div v-if="!data.by_project.length" class="empty-row">Aucune donnée pour ce mois</div>
        <table v-else class="cost-table">
          <thead>
            <tr>
              <th>Projet</th>
              <th>Équipe</th>
              <th class="num">Heures container</th>
              <th class="num">Coût container</th>
              <th class="num">Min CI</th>
              <th class="num">Coût CI</th>
              <th class="num total-col">Total</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in data.by_project" :key="p.project">
              <td class="name-cell">{{ p.project }}</td>
              <td class="dim">{{ p.namespace || '—' }}</td>
              <td class="num dim">{{ p.container_hours }}h</td>
              <td class="num">{{ fmt(p.container_cost) }}</td>
              <td class="num dim">{{ p.ci_minutes }}min</td>
              <td class="num">{{ fmt(p.ci_cost) }}</td>
              <td class="num total-col">
                <strong>{{ fmt(p.total) }} {{ data.currency }}</strong>
              </td>
            </tr>
          </tbody>
          <tfoot>
            <tr class="total-row">
              <td colspan="6">Total</td>
              <td class="num total-col"><strong>{{ fmt(data.total) }} {{ data.currency }}</strong></td>
            </tr>
          </tfoot>
        </table>
      </section>

      <!-- Par équipe -->
      <section class="section" v-if="data.by_team.length > 1">
        <h3 class="section-title">👥 Par équipe (namespace)</h3>
        <table class="cost-table">
          <thead>
            <tr>
              <th>Équipe</th>
              <th class="num">Coût containers</th>
              <th class="num">Coût CI</th>
              <th class="num total-col">Total</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in data.by_team" :key="t.namespace">
              <td class="name-cell">{{ t.namespace }}</td>
              <td class="num">{{ fmt(t.container_cost) }}</td>
              <td class="num">{{ fmt(t.ci_cost) }}</td>
              <td class="num total-col">
                <strong>{{ fmt(t.total) }} {{ data.currency }}</strong>
              </td>
            </tr>
          </tbody>
        </table>
        <!-- Répartition visuelle -->
        <div class="bar-chart" v-if="data.total > 0">
          <div
            v-for="t in data.by_team"
            :key="t.namespace"
            class="bar-segment"
            :style="{ width: pct(t.total) + '%', background: teamColor(t.namespace) }"
            :title="t.namespace + ' — ' + fmt(t.total) + ' ' + data.currency"
          ></div>
        </div>
        <div class="bar-legend" v-if="data.total > 0">
          <div v-for="t in data.by_team" :key="t.namespace" class="legend-item">
            <span class="legend-dot" :style="{ background: teamColor(t.namespace) }"></span>
            {{ t.namespace }} ({{ pct(t.total) }}%)
          </div>
        </div>
      </section>

      <!-- Info tarifs -->
      <div class="rates-info" v-if="data.total === 0 && data.by_project.length">
        <span class="rates-icon">ℹ️</span>
        Usage tracké (containers + CI), mais tarifs à zéro.
        Copiez <code>config/costs.example.yaml</code> → <code>config/costs.yaml</code> et redémarrez Symphony pour valoriser l'usage.
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

const data = ref(null)
const loading = ref(true)
const error = ref(null)

// Navigation mois
const now = new Date()
const currentYear = ref(now.getFullYear())
const currentMonth = ref(now.getMonth() + 1) // 1-12

const isCurrentMonth = computed(() =>
  currentYear.value === now.getFullYear() && currentMonth.value === now.getMonth() + 1
)

const monthStr = computed(() =>
  `${currentYear.value}-${String(currentMonth.value).padStart(2, '0')}`
)

const periodDisplay = computed(() => {
  const d = new Date(currentYear.value, currentMonth.value - 1, 1)
  return d.toLocaleDateString('fr-FR', { month: 'long', year: 'numeric' })
})

function prevMonth() {
  if (currentMonth.value === 1) { currentYear.value--; currentMonth.value = 12 }
  else currentMonth.value--
  load()
}

function nextMonth() {
  if (isCurrentMonth.value) return
  if (currentMonth.value === 12) { currentYear.value++; currentMonth.value = 1 }
  else currentMonth.value++
  load()
}

// Formatage
const fmt = (n) => (n || 0).toFixed(2)
const pct = (n) => data.value?.total > 0 ? Math.round((n / data.value.total) * 100) : 0

const TEAM_COLORS = ['#667eea', '#48bb78', '#ed8936', '#e53e3e', '#9f7aea', '#38b2ac', '#f6ad55']
const teamColorCache = {}
let colorIdx = 0
function teamColor(ns) {
  if (!teamColorCache[ns]) teamColorCache[ns] = TEAM_COLORS[colorIdx++ % TEAM_COLORS.length]
  return teamColorCache[ns]
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const { data: d } = await api.getCosts(monthStr.value)
    data.value = d
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }

.header-controls { display: flex; align-items: center; gap: 8px; }
.btn-nav { background: white; border: 1px solid #ddd; border-radius: 6px; padding: 6px 12px; font-size: 16px; cursor: pointer; }
.btn-nav:disabled { opacity: .4; cursor: not-allowed; }
.btn-nav:not(:disabled):hover { border-color: #667eea; color: #667eea; }
.period-label { font-size: 15px; font-weight: 600; min-width: 140px; text-align: center; }
.btn-refresh { background: white; border: 1px solid #ddd; border-radius: 6px; padding: 6px 12px; cursor: pointer; }
.btn-refresh:hover { border-color: #667eea; }

.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.skeleton-block { height: 200px; background: linear-gradient(90deg,#f0f2f5 25%,#e8eaf0 50%,#f0f2f5 75%); background-size: 200% 100%; animation: shimmer 1.4s infinite; border-radius: 12px; }
@keyframes shimmer { 0%{background-position:200% 0} 100%{background-position:-200% 0} }

.total-banner {
  background: #1a1a2e;
  color: white;
  border-radius: 14px;
  padding: 24px 28px;
  margin-bottom: 24px;
  display: flex;
  align-items: center;
  gap: 24px;
}
.total-label { font-size: 14px; color: #aaa; flex: 1; }
.total-val { font-size: 32px; font-weight: 700; }
.total-sub { font-size: 12px; color: #888; }
.total-sub code { background: #ffffff15; padding: 1px 6px; border-radius: 4px; font-family: monospace; }

.section { background: white; border: 1px solid #e5e7eb; border-radius: 12px; overflow: hidden; margin-bottom: 20px; }
.section-title { font-size: 15px; font-weight: 700; padding: 16px 20px; border-bottom: 1px solid #f0f0f0; }
.empty-row { padding: 20px; color: #aaa; font-size: 14px; }

.cost-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.cost-table th { background: #f8f9fb; color: #888; font-weight: 600; padding: 10px 16px; text-align: left; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
.cost-table th.num { text-align: right; }
.cost-table td { padding: 10px 16px; border-top: 1px solid #f5f5f5; }
.cost-table tr:hover td { background: #fafbfc; }
.cost-table tfoot td { background: #f8f9fb; font-weight: 600; border-top: 2px solid #e5e7eb; }

.num { text-align: right; }
.total-col { background: #fafbff; }
.name-cell { font-weight: 600; }
.dim { color: #888; }

.total-row td { background: #f0f4ff; color: #1a1a1a; }

.bar-chart {
  height: 16px;
  display: flex;
  margin: 16px 20px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #f0f2f5;
}
.bar-segment { height: 100%; transition: width .3s ease; }

.bar-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 20px 16px;
  font-size: 12px;
  color: #555;
}
.legend-item { display: flex; align-items: center; gap: 6px; }
.legend-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }

.rates-info {
  background: #fffbeb;
  border: 1px solid #fcd34d;
  border-radius: 10px;
  padding: 14px 16px;
  font-size: 13px;
  color: #92400e;
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.rates-icon { font-size: 16px; flex-shrink: 0; }
.rates-info code { font-family: monospace; background: #fef3c7; padding: 1px 5px; border-radius: 3px; }
</style>
