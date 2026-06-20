<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Catalogue de services</h2>
        <p class="subtitle">{{ filtered.length }} service(s) — mis à jour automatiquement via GitOps</p>
      </div>
      <div class="toolbar">
        <input v-model="search" placeholder="🔍 Rechercher..." class="search" />
        <select v-model="filterTier" class="filter">
          <option value="">Tous les tiers</option>
          <option value="critical">Critical</option>
          <option value="standard">Standard</option>
          <option value="internal">Internal</option>
        </select>
        <select v-model="filterLifecycle" class="filter">
          <option value="">Tous les états</option>
          <option value="production">Production</option>
          <option value="staging">Staging</option>
          <option value="deprecated">Deprecated</option>
        </select>
      </div>
    </div>

    <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

    <div v-if="loading" class="state">Chargement...</div>
    <div v-else-if="!services.length" class="state">
      Aucun service dans <code>symphony-config/services/</code>
    </div>

    <div class="grid" v-else>
      <div v-for="svc in filtered" :key="svc.metadata.name"
        class="card" :class="{ active: selected?.metadata.name === svc.metadata.name }"
        @click="select(svc)">

        <div class="card-top">
          <div class="card-name-row">
            <span class="svc-name">{{ svc.metadata.name }}</span>
            <span :class="['tier-badge', svc.metadata.tier]">
              {{ tierIcon(svc.metadata.tier) }} {{ svc.metadata.tier || 'standard' }}
            </span>
          </div>
          <span :class="['lifecycle-badge', svc.metadata.lifecycle]">
            {{ svc.metadata.lifecycle || 'production' }}
          </span>
        </div>

        <div class="svc-meta">
          <span>👤 {{ svc.metadata.owner }}</span>
          <span v-if="svc.spec.language">{{ langIcon(svc.spec.language) }} {{ svc.spec.language }}</span>
          <span v-if="svc.spec.version">v{{ svc.spec.version }}</span>
        </div>

        <div class="slo-row" v-if="svc.spec.slo?.availability">
          <span class="slo-item">🟢 {{ svc.spec.slo.availability }}</span>
          <span class="slo-item" v-if="svc.spec.slo.latency_p99">⚡ {{ svc.spec.slo.latency_p99 }}</span>
        </div>

        <div class="tags">
          <span class="tag" v-for="t in svc.metadata.tags?.slice(0,3)" :key="t">{{ t }}</span>
        </div>
      </div>
    </div>

    <!-- Panneau détail -->
    <Transition name="slide">
      <aside class="panel" v-if="selected">
        <div class="panel-header">
          <div>
            <h3>{{ selected.metadata.name }}</h3>
            <div class="panel-badges">
              <span :class="['tier-badge', selected.metadata.tier]">
                {{ tierIcon(selected.metadata.tier) }} {{ selected.metadata.tier || 'standard' }}
              </span>
              <span :class="['lifecycle-badge', selected.metadata.lifecycle]">
                {{ selected.metadata.lifecycle || 'production' }}
              </span>
            </div>
          </div>
          <button class="close" @click="selected = null">✕</button>
        </div>

        <!-- Team -->
        <section>
          <h4>Équipe</h4>
          <div class="info-row">
            <span>👤 Owner</span>
            <strong>{{ selected.metadata.owner }}</strong>
          </div>
          <div class="info-row" v-if="selected.metadata.team?.slack">
            <span>💬 Slack</span>
            <code>{{ selected.metadata.team.slack }}</code>
          </div>
          <div class="info-row" v-if="selected.metadata.team?.email">
            <span>📧 Email</span>
            <a :href="'mailto:'+selected.metadata.team.email">{{ selected.metadata.team.email }}</a>
          </div>
        </section>

        <!-- Tech -->
        <section v-if="selected.spec.language || selected.spec.version">
          <h4>Stack technique</h4>
          <div class="info-row" v-if="selected.spec.language">
            <span>Langage</span>
            <strong>{{ langIcon(selected.spec.language) }} {{ selected.spec.language }}</strong>
          </div>
          <div class="info-row" v-if="selected.spec.type">
            <span>Type</span>
            <strong>{{ selected.spec.type }}</strong>
          </div>
          <div class="info-row" v-if="selected.spec.version">
            <span>Version</span>
            <code>v{{ selected.spec.version }}</code>
          </div>
          <div class="info-row" v-if="selected.spec.registry">
            <span>Registry</span>
            <code>{{ selected.spec.registry }}</code>
          </div>
        </section>

        <!-- SLO -->
        <section v-if="selected.spec.slo?.availability">
          <h4>SLO</h4>
          <div class="slo-grid">
            <div class="slo-card">
              <div class="slo-value">{{ selected.spec.slo.availability }}</div>
              <div class="slo-label">Disponibilité</div>
            </div>
            <div class="slo-card" v-if="selected.spec.slo.latency_p99">
              <div class="slo-value">{{ selected.spec.slo.latency_p99 }}</div>
              <div class="slo-label">Latence p99</div>
            </div>
          </div>
        </section>

        <!-- Dépendances -->
        <section v-if="selected.spec.dependencies?.length">
          <h4>Dépendances</h4>
          <div class="dep-item" v-for="dep in selected.spec.dependencies" :key="dep.service">
            <span class="dep-type">{{ depIcon(dep.type) }} {{ dep.type }}</span>
            <span class="dep-name">{{ dep.service }}</span>
          </div>
        </section>

        <!-- Liens -->
        <section v-if="selected.spec.links?.length">
          <h4>Liens</h4>
          <a v-for="l in selected.spec.links" :key="l.title"
            :href="l.url" target="_blank" class="link-item">
            {{ linkIcon(l.icon) }} {{ l.title }}
            <span class="link-arrow">↗</span>
          </a>
        </section>

        <!-- Actions -->
        <section v-if="selected.spec.actions?.length">
          <h4>Actions</h4>
          <div v-for="action in selected.spec.actions" :key="action.name" class="action-block">
            <div class="action-title">⚡ {{ action.name }}</div>
            <div v-for="input in action.inputs" :key="input.id" class="field">
              <label>{{ input.id }}</label>
              <input v-if="input.type === 'integer'" type="number"
                :min="input.min" :max="input.max"
                v-model="vals[action.name + input.id]" />
              <input v-else type="text"
                v-model="vals[action.name + input.id]"
                :placeholder="input.default || input.id" />
            </div>
            <button class="btn" :disabled="busy[action.name]" @click="trigger(action)">
              {{ busy[action.name] ? '⏳ Envoi...' : 'Déclencher' }}
            </button>
            <div v-if="feedback[action.name]"
              :class="['feedback', feedback[action.name].ok ? 'ok' : 'err']">
              {{ feedback[action.name].msg }}
            </div>
          </div>
        </section>
      </aside>
    </Transition>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

const services = ref([])
const loading = ref(true)
const error = ref(null)
const search = ref('')
const filterTier = ref('')
const filterLifecycle = ref('')
const selected = ref(null)
const vals = ref({})
const busy = ref({})
const feedback = ref({})

const filtered = computed(() =>
  services.value.filter(s => {
    const q = search.value.toLowerCase()
    const matchSearch = !q || [
      s.metadata.name,
      s.metadata.owner,
      s.spec.language,
      ...(s.metadata.tags || [])
    ].some(v => v?.toLowerCase().includes(q))

    const matchTier = !filterTier.value || s.metadata.tier === filterTier.value
    const matchLifecycle = !filterLifecycle.value || s.metadata.lifecycle === filterLifecycle.value

    return matchSearch && matchTier && matchLifecycle
  })
)

const tierIcon = (tier) => ({ critical: '🔴', standard: '🟡', internal: '⚪' })[tier] || '🟡'
const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const depIcon = (type) => ({ database: '🗄️', cache: '⚡', api: '🔌', queue: '📨' })[type] || '📦'
const linkIcon = (icon) => ({ monitoring: '📊', errors: '🐛', docs: '📖', code: '💻', ci: '⚙️', web: '🌐', docker: '🐳' })[icon] || '🔗'

function select(svc) {
  selected.value = selected.value?.metadata.name === svc.metadata.name ? null : svc
}

onMounted(async () => {
  try {
    const { data } = await api.getServices()
    services.value = data || []
    error.value = null
  } catch (e) {
    services.value = []
    error.value = e.response?.data?.error || e.message
  } finally { loading.value = false }
})

async function trigger(action) {
  const inputs = {}
  action.inputs?.forEach(i => { inputs[i.id] = vals.value[action.name + i.id] })
  busy.value[action.name] = true
  feedback.value[action.name] = null
  try {
    await api.triggerAction(selected.value.metadata.name, action.name, inputs)
    feedback.value[action.name] = { ok: true, msg: '✅ Action déclenchée' }
  } catch (e) {
    feedback.value[action.name] = { ok: false, msg: '❌ ' + (e.response?.data?.error || e.message) }
  } finally { busy.value[action.name] = false }
}
</script>

<style scoped>
.page-header { margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; margin-bottom: 4px; }
.subtitle { color: #888; font-size: 13px; margin-bottom: 14px; }
.toolbar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.search { flex: 1; min-width: 200px; padding: 9px 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; }
.filter { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; cursor: pointer; }
.state { text-align: center; padding: 60px; color: #888; }
.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 14px; }
.card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; cursor: pointer; transition: all .15s; }
.card:hover { border-color: #667eea; transform: translateY(-1px); box-shadow: 0 4px 12px #667eea15; }
.card.active { border-color: #667eea; box-shadow: 0 0 0 2px #667eea33; }
.card-top { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 10px; }
.card-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.svc-name { font-weight: 700; font-size: 15px; }
.tier-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; font-weight: 500; }
.tier-badge.critical { background: #fff5f5; color: #c53030; }
.tier-badge.standard { background: #fffbeb; color: #92400e; }
.tier-badge.internal { background: #f4f4f4; color: #666; }
.lifecycle-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; }
.lifecycle-badge.production { background: #f0fff4; color: #276749; }
.lifecycle-badge.staging { background: #ebf8ff; color: #2b6cb0; }
.lifecycle-badge.deprecated { background: #f4f4f4; color: #999; text-decoration: line-through; }
.svc-meta { display: flex; gap: 10px; font-size: 13px; color: #666; margin-bottom: 8px; flex-wrap: wrap; }
.slo-row { display: flex; gap: 10px; margin-bottom: 8px; }
.slo-item { font-size: 12px; color: #276749; background: #f0fff4; padding: 2px 8px; border-radius: 20px; }
.tags { display: flex; gap: 6px; flex-wrap: wrap; }
.tag { font-size: 11px; background: #f4f4f4; padding: 2px 8px; border-radius: 20px; color: #555; }

/* Panel */
.panel { position: fixed; right: 0; top: 56px; width: 400px; height: calc(100vh - 56px); background: white; border-left: 1px solid #e2e2e2; padding: 24px; overflow-y: auto; box-shadow: -8px 0 24px #0001; z-index: 50; }
.panel-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
.panel-header h3 { font-size: 20px; font-weight: 700; margin-bottom: 6px; }
.panel-badges { display: flex; gap: 6px; }
.close { background: none; border: none; font-size: 20px; cursor: pointer; color: #aaa; }
section { margin-bottom: 22px; padding-bottom: 22px; border-bottom: 1px solid #f0f0f0; }
section:last-child { border-bottom: none; }
h4 { font-size: 11px; text-transform: uppercase; color: #aaa; letter-spacing: .08em; margin-bottom: 10px; font-weight: 600; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 6px 0; font-size: 14px; }
.info-row span { color: #888; }
.info-row a { color: #667eea; text-decoration: none; font-size: 13px; }
.info-row code { font-size: 12px; background: #f4f4f4; padding: 2px 6px; border-radius: 4px; }
.slo-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.slo-card { background: #f0fff4; border-radius: 10px; padding: 12px; text-align: center; }
.slo-value { font-size: 18px; font-weight: 700; color: #276749; }
.slo-label { font-size: 11px; color: #888; margin-top: 2px; }
.dep-item { display: flex; justify-content: space-between; padding: 8px 12px; background: #f8f9fb; border-radius: 8px; margin-bottom: 6px; font-size: 14px; }
.dep-type { color: #888; font-size: 13px; }
.dep-name { font-weight: 500; }
.link-item { display: flex; justify-content: space-between; align-items: center; color: #667eea; text-decoration: none; padding: 10px 12px; border-radius: 8px; font-size: 14px; margin-bottom: 4px; transition: background .1s; }
.link-item:hover { background: #f0f4ff; }
.link-arrow { color: #aaa; }
.action-block { background: #f8f9fb; border-radius: 10px; padding: 14px; margin-bottom: 10px; }
.action-title { font-weight: 600; font-size: 14px; margin-bottom: 10px; }
.field { margin-bottom: 8px; }
.field label { display: block; font-size: 12px; color: #888; margin-bottom: 4px; font-weight: 500; }
.field input { width: 100%; padding: 8px 10px; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; }
.btn { width: 100%; padding: 9px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; margin-top: 6px; }
.btn:disabled { opacity: .5; cursor: not-allowed; }
.feedback { margin-top: 8px; padding: 8px 12px; border-radius: 6px; font-size: 13px; }
.feedback.ok { background: #f0fff4; color: #276749; }
.feedback.err { background: #fff5f5; color: #c53030; }
.slide-enter-active, .slide-leave-active { transition: transform .2s ease, opacity .2s; }
.slide-enter-from, .slide-leave-to { transform: translateX(20px); opacity: 0; }
</style>
