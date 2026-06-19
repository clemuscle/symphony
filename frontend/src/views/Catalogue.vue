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

    <div v-if="loading" class="state">Chargement...</div>
    <div v-else-if="!services.length" class="state">
      Aucun service dans <code>symphony-config/services/</code>
    </div>

    <div class="grid" v-else>
      <div v-for="svc in filtered" :key="svc.Metadata.Name"
        class="card" :class="{ active: selected?.Metadata.Name === svc.Metadata.Name }"
        @click="select(svc)">

        <div class="card-top">
          <div class="card-name-row">
            <span class="svc-name">{{ svc.Metadata.Name }}</span>
            <span :class="['tier-badge', svc.Metadata.Tier]">
              {{ tierIcon(svc.Metadata.Tier) }} {{ svc.Metadata.Tier || 'standard' }}
            </span>
          </div>
          <span :class="['lifecycle-badge', svc.Metadata.Lifecycle]">
            {{ svc.Metadata.Lifecycle || 'production' }}
          </span>
        </div>

        <div class="svc-meta">
          <span>👤 {{ svc.Metadata.Owner }}</span>
          <span v-if="svc.Spec.Language">{{ langIcon(svc.Spec.Language) }} {{ svc.Spec.Language }}</span>
          <span v-if="svc.Spec.Version">v{{ svc.Spec.Version }}</span>
        </div>

        <div class="slo-row" v-if="svc.Spec.SLO?.Availability">
          <span class="slo-item">🟢 {{ svc.Spec.SLO.Availability }}</span>
          <span class="slo-item" v-if="svc.Spec.SLO.LatencyP99">⚡ {{ svc.Spec.SLO.LatencyP99 }}</span>
        </div>

        <div class="tags">
          <span class="tag" v-for="t in svc.Metadata.Tags?.slice(0,3)" :key="t">{{ t }}</span>
        </div>
      </div>
    </div>

    <!-- Panneau détail -->
    <Transition name="slide">
      <aside class="panel" v-if="selected">
        <div class="panel-header">
          <div>
            <h3>{{ selected.Metadata.Name }}</h3>
            <div class="panel-badges">
              <span :class="['tier-badge', selected.Metadata.Tier]">
                {{ tierIcon(selected.Metadata.Tier) }} {{ selected.Metadata.Tier || 'standard' }}
              </span>
              <span :class="['lifecycle-badge', selected.Metadata.Lifecycle]">
                {{ selected.Metadata.Lifecycle || 'production' }}
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
            <strong>{{ selected.Metadata.Owner }}</strong>
          </div>
          <div class="info-row" v-if="selected.Metadata.Team?.Slack">
            <span>💬 Slack</span>
            <code>{{ selected.Metadata.Team.Slack }}</code>
          </div>
          <div class="info-row" v-if="selected.Metadata.Team?.Email">
            <span>📧 Email</span>
            <a :href="'mailto:'+selected.Metadata.Team.Email">{{ selected.Metadata.Team.Email }}</a>
          </div>
        </section>

        <!-- Tech -->
        <section v-if="selected.Spec.Language || selected.Spec.Version">
          <h4>Stack technique</h4>
          <div class="info-row" v-if="selected.Spec.Language">
            <span>Langage</span>
            <strong>{{ langIcon(selected.Spec.Language) }} {{ selected.Spec.Language }}</strong>
          </div>
          <div class="info-row" v-if="selected.Spec.Type">
            <span>Type</span>
            <strong>{{ selected.Spec.Type }}</strong>
          </div>
          <div class="info-row" v-if="selected.Spec.Version">
            <span>Version</span>
            <code>v{{ selected.Spec.Version }}</code>
          </div>
          <div class="info-row" v-if="selected.Spec.Registry">
            <span>Registry</span>
            <code>{{ selected.Spec.Registry }}</code>
          </div>
        </section>

        <!-- SLO -->
        <section v-if="selected.Spec.SLO?.Availability">
          <h4>SLO</h4>
          <div class="slo-grid">
            <div class="slo-card">
              <div class="slo-value">{{ selected.Spec.SLO.Availability }}</div>
              <div class="slo-label">Disponibilité</div>
            </div>
            <div class="slo-card" v-if="selected.Spec.SLO.LatencyP99">
              <div class="slo-value">{{ selected.Spec.SLO.LatencyP99 }}</div>
              <div class="slo-label">Latence p99</div>
            </div>
          </div>
        </section>

        <!-- Dépendances -->
        <section v-if="selected.Spec.Dependencies?.length">
          <h4>Dépendances</h4>
          <div class="dep-item" v-for="dep in selected.Spec.Dependencies" :key="dep.Service">
            <span class="dep-type">{{ depIcon(dep.Type) }} {{ dep.Type }}</span>
            <span class="dep-name">{{ dep.Service }}</span>
          </div>
        </section>

        <!-- Liens -->
        <section v-if="selected.Spec.Links?.length">
          <h4>Liens</h4>
          <a v-for="l in selected.Spec.Links" :key="l.Title"
            :href="l.URL" target="_blank" class="link-item">
            {{ linkIcon(l.Icon) }} {{ l.Title }}
            <span class="link-arrow">↗</span>
          </a>
        </section>

        <!-- Actions -->
        <section v-if="selected.Spec.Actions?.length">
          <h4>Actions</h4>
          <div v-for="action in selected.Spec.Actions" :key="action.Name" class="action-block">
            <div class="action-title">⚡ {{ action.Name }}</div>
            <div v-for="input in action.Inputs" :key="input.ID" class="field">
              <label>{{ input.ID }}</label>
              <input v-if="input.Type === 'integer'" type="number"
                :min="input.Min" :max="input.Max"
                v-model="vals[action.Name + input.ID]" />
              <input v-else type="text"
                v-model="vals[action.Name + input.ID]"
                :placeholder="input.Default || input.ID" />
            </div>
            <button class="btn" :disabled="busy[action.Name]" @click="trigger(action)">
              {{ busy[action.Name] ? '⏳ Envoi...' : 'Déclencher' }}
            </button>
            <div v-if="feedback[action.Name]"
              :class="['feedback', feedback[action.Name].ok ? 'ok' : 'err']">
              {{ feedback[action.Name].msg }}
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
      s.Metadata.Name,
      s.Metadata.Owner,
      s.Spec.Language,
      ...(s.Metadata.Tags || [])
    ].some(v => v?.toLowerCase().includes(q))

    const matchTier = !filterTier.value || s.Metadata.Tier === filterTier.value
    const matchLifecycle = !filterLifecycle.value || s.Metadata.Lifecycle === filterLifecycle.value

    return matchSearch && matchTier && matchLifecycle
  })
)

const tierIcon = (tier) => ({ critical: '🔴', standard: '🟡', internal: '⚪' })[tier] || '🟡'
const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const depIcon = (type) => ({ database: '🗄️', cache: '⚡', api: '🔌', queue: '📨' })[type] || '📦'
const linkIcon = (icon) => ({ monitoring: '📊', errors: '🐛', docs: '📖', code: '💻', ci: '⚙️', web: '🌐', docker: '🐳' })[icon] || '🔗'

function select(svc) {
  selected.value = selected.value?.Metadata.Name === svc.Metadata.Name ? null : svc
}

onMounted(async () => {
  try {
    const { data } = await api.getServices()
    services.value = data || []
  } catch { services.value = [] }
  finally { loading.value = false }
})

async function trigger(action) {
  const inputs = {}
  action.Inputs?.forEach(i => { inputs[i.ID] = vals.value[action.Name + i.ID] })
  busy.value[action.Name] = true
  feedback.value[action.Name] = null
  try {
    await api.triggerAction(selected.value.Metadata.Name, action.Name, inputs)
    feedback.value[action.Name] = { ok: true, msg: '✅ Action déclenchée' }
  } catch (e) {
    feedback.value[action.Name] = { ok: false, msg: '❌ ' + (e.response?.data?.error || e.message) }
  } finally { busy.value[action.Name] = false }
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
