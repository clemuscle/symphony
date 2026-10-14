<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Projets</h2>
        <p class="subtitle">
          {{ filtered.length }} projet(s) — chacun a son repo, sa CI et sa fiche
          <code>catalog-info.yaml</code>, relue toutes les 15 s
        </p>
      </div>
      <RouterLink to="/catalogue" class="btn-primary" v-if="can('developer')">+ Nouveau projet</RouterLink>
    </div>

    <div class="toolbar" v-if="projects.length">
      <input v-model="search" placeholder="🔍 Rechercher (nom, équipe, langage, tag)..." class="search" />
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
        <option value="experimental">Experimental</option>
        <option value="deprecated">Deprecated</option>
      </select>
    </div>

    <div v-if="loading" class="state">Chargement...</div>
    <div v-else-if="!projects.length" class="state">
      Aucun projet pour l'instant.
      <RouterLink to="/catalogue">Choisir un golden path dans le catalogue →</RouterLink>
    </div>

    <div class="grid" v-else>
      <div v-for="p in filtered" :key="p.id"
        class="card" :class="{ active: selectedName === p.name }"
        @click="select(p)">
        <div class="card-top">
          <div class="card-name-row">
            <span class="svc-name">{{ p.name }}</span>
            <span :class="['tier-badge', tier(p)]">{{ tierIcon(tier(p)) }} {{ tier(p) }}</span>
          </div>
          <span :class="['lifecycle-badge', lifecycle(p)]" v-if="lifecycle(p)">{{ lifecycle(p) }}</span>
        </div>
        <div class="card-desc">{{ p.description || 'Pas de description' }}</div>
        <div class="svc-meta">
          <span v-if="owner(p)">👤 {{ owner(p) }}</span>
          <span>{{ langIcon(p.language) }} {{ p.language }}</span>
          <span class="muted">{{ p.golden_path }}</span>
        </div>

        <div class="deploy-line" v-for="d in deploymentsOf(p)" :key="d.provider + d.id">
          <span :class="['dot', d.status]"></span>
          <span class="deploy-rt">{{ runtimeLabel(d.provider) }}</span>
          <a v-if="d.url && d.status === 'running'" :href="d.url" target="_blank" @click.stop>{{ shortURL(d.url) }} ↗</a>
          <span v-else class="muted">{{ d.status }}</span>
        </div>
        <div class="deploy-line muted" v-if="!deploymentsOf(p).length && !activeTaskFor(p.name)">Pas encore déployé</div>

        <div class="project-task" v-if="activeTaskFor(p.name)">
          ⚡ {{ kindLabels[activeTaskFor(p.name).kind] }} :
          {{ activeTaskFor(p.name).steps[activeTaskFor(p.name).step] }}
        </div>
        <div class="no-fiche" v-else-if="!p.service">Fiche catalogue absente du repo</div>
      </div>
      <div class="empty" v-if="!filtered.length">Aucun projet ne correspond aux filtres</div>
    </div>

    <!-- Panneau détail : le projet et sa fiche -->
    <Transition name="slide">
      <aside class="panel" v-if="selected">
        <div class="panel-header">
          <div>
            <h3>{{ selected.name }}</h3>
            <div class="panel-badges">
              <span :class="['tier-badge', tier(selected)]">{{ tierIcon(tier(selected)) }} {{ tier(selected) }}</span>
              <span :class="['lifecycle-badge', lifecycle(selected)]" v-if="lifecycle(selected)">{{ lifecycle(selected) }}</span>
            </div>
          </div>
          <button class="close" @click="selectedName = null">✕</button>
        </div>
        <p class="panel-desc" v-if="selected.description">{{ selected.description }}</p>

        <section>
          <h4>Code et CI</h4>
          <div class="btn-row">
            <a :href="repoWebURL(selected)" target="_blank" class="btn-ghost">💻 Repo ↗</a>
            <a v-if="pipelinesURL(selected)" :href="pipelinesURL(selected)" target="_blank" class="btn-ghost">⚙️ Pipelines ↗</a>
            <!-- Avec une fiche, la relance passe par ses actions Day-2 -->
            <button class="btn-ghost" v-if="can('developer') && !svc" @click="triggerPipeline(selected)">▶ Relancer la pipeline</button>
          </div>
          <div class="pipeline-status" v-if="pipelines[selected.repo_path]">
            <span>Pipeline #{{ pipelines[selected.repo_path].id }}</span>
            <span :class="['status-badge', pipelines[selected.repo_path].status]">{{ pipelines[selected.repo_path].status }}</span>
          </div>
          <div class="info-row"><span>Repo</span><code>{{ selected.repo_path }}</code></div>
          <div class="info-row"><span>Golden path</span><code>{{ selected.golden_path }}</code></div>
          <div class="info-row" v-if="selected.created_by"><span>Créé par</span><strong>{{ selected.created_by }}</strong></div>
        </section>

        <section>
          <h4>Déploiements</h4>
          <div class="deploy-item" v-for="d in deploymentsOf(selected)" :key="d.provider + d.id">
            <div class="deploy-head">
              <span><span :class="['dot', d.status]"></span> {{ runtimeLabel(d.provider) }}</span>
              <span class="muted">{{ d.status }}<template v-if="d.sync"> · {{ d.sync }} / {{ d.health }}</template></span>
            </div>
            <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} ↗</a>
            <code class="deploy-image">{{ d.image }}</code>
          </div>
          <div class="muted small" v-if="!deploymentsOf(selected).length">Aucun déploiement.</div>
          <RouterLink v-if="can('developer')" :to="{ path: '/deployments', query: { project: selected.name } }" class="btn-ghost block">
            🚀 Déployer une version
          </RouterLink>
        </section>

        <template v-if="svc">
          <section>
            <h4>Équipe</h4>
            <div class="info-row"><span>👤 Owner</span><strong>{{ svc.metadata.owner }}</strong></div>
            <div class="info-row" v-if="svc.metadata.team?.slack"><span>💬 Slack</span><code>{{ svc.metadata.team.slack }}</code></div>
            <div class="info-row" v-if="svc.metadata.team?.email">
              <span>📧 Email</span><a :href="'mailto:' + svc.metadata.team.email">{{ svc.metadata.team.email }}</a>
            </div>
            <div class="tags" v-if="svc.metadata.tags?.length">
              <span class="tag" v-for="t in svc.metadata.tags" :key="t">{{ t }}</span>
            </div>
          </section>

          <section v-if="svc.spec.slo?.availability">
            <h4>SLO</h4>
            <div class="slo-grid">
              <div class="slo-card">
                <div class="slo-value">{{ svc.spec.slo.availability }}</div>
                <div class="slo-label">Disponibilité</div>
              </div>
              <div class="slo-card" v-if="svc.spec.slo.latency_p99">
                <div class="slo-value">{{ svc.spec.slo.latency_p99 }}</div>
                <div class="slo-label">Latence p99</div>
              </div>
            </div>
          </section>

          <section v-if="svc.spec.dependencies?.length">
            <h4>Dépendances</h4>
            <div class="dep-item" v-for="dep in svc.spec.dependencies" :key="dep.service">
              <span class="dep-type">{{ depIcon(dep.type) }} {{ dep.type }}</span>
              <span class="dep-name">{{ dep.service }}</span>
            </div>
          </section>

          <section v-if="extraLinks.length">
            <h4>Liens</h4>
            <a v-for="l in extraLinks" :key="l.title" :href="l.url" target="_blank" class="link-item">
              {{ linkIcon(l.icon) }} {{ l.title }} <span class="link-arrow">↗</span>
            </a>
          </section>

          <section v-if="svc.spec.actions?.length">
            <h4>Actions Day-2</h4>
            <div v-for="action in svc.spec.actions" :key="action.name" class="action-block">
              <div class="action-title">⚡ {{ action.name }}</div>
              <div class="action-desc" v-if="action.description">{{ action.description }}</div>
              <div v-for="input in action.inputs" :key="input.id" class="field">
                <label>{{ input.label || input.id }}</label>
                <select v-if="input.options?.length" v-model="vals[action.name + input.id]">
                  <option v-for="o in input.options" :key="o" :value="o">{{ o }}</option>
                </select>
                <input v-else-if="input.type === 'integer'" type="number" :min="input.min" :max="input.max"
                  v-model="vals[action.name + input.id]" :placeholder="input.default" />
                <input v-else type="text" v-model="vals[action.name + input.id]" :placeholder="input.default || input.id" />
              </div>
              <button class="btn" v-if="can('developer')" :disabled="busy[action.name]" @click="trigger(action)">
                {{ busy[action.name] ? '⏳ Envoi...' : 'Déclencher' }}
              </button>
              <TaskProgress class="action-task" v-if="feedback[action.name]?.taskId && taskStore.byId[feedback[action.name].taskId]"
                :task="taskStore.byId[feedback[action.name].taskId]" />
              <div v-else-if="feedback[action.name]" :class="['feedback', feedback[action.name].ok ? 'ok' : 'err']">
                {{ feedback[action.name].msg }}
              </div>
            </div>
          </section>
        </template>

        <section>
          <h4>Fiche catalogue</h4>
          <p class="muted small">
            Les métadonnées (équipe, tier, SLO, liens, actions) viennent de
            <code>catalog-info.yaml</code> dans le repo : l'équipe les modifie par un commit.
          </p>
          <a :href="ficheURL(selected)" target="_blank" class="btn-ghost block">✏️ Modifier catalog-info.yaml ↗</a>
        </section>

        <section v-if="can('admin')">
          <h4>Zone dangereuse</h4>
          <button class="btn-ghost btn-danger block" :disabled="isDeleting(selected)" @click="deleteProject(selected)">
            {{ isDeleting(selected) ? 'Suppression…' : '🗑 Supprimer le projet' }}
          </button>
        </section>
      </aside>
    </Transition>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { can } from '../stores/session'
import TaskProgress from '../components/TaskProgress.vue'
import { taskStore, trackTask, activeTasks, activeTaskFor, kindLabels } from '../stores/tasks'

const route = useRoute()
const projects = ref([])
const deployments = ref([])
const loading = ref(true)
const search = ref('')
const filterTier = ref('')
const filterLifecycle = ref('')
const selectedName = ref(route.query.project || null)
const pipelines = ref({})
const vals = ref({})
const busy = ref({})
const feedback = ref({})

const selected = computed(() => projects.value.find(p => p.name === selectedName.value))
const svc = computed(() => selected.value?.service)
// Repo et Pipelines ont déjà leurs boutons
const extraLinks = computed(() => (svc.value?.spec.links || []).filter(l => !['Repository', 'Pipelines'].includes(l.title)))

const tier = (p) => p.service?.metadata.tier || 'standard'
const lifecycle = (p) => p.service?.metadata.lifecycle || ''
const owner = (p) => p.service?.metadata.owner || ''

const filtered = computed(() => projects.value.filter(p => {
  const q = search.value.toLowerCase()
  const matchSearch = !q || [p.name, p.description, owner(p), p.language, ...(p.service?.metadata.tags || [])]
    .some(v => v?.toLowerCase().includes(q))
  return matchSearch
    && (!filterTier.value || tier(p) === filterTier.value)
    && (!filterLifecycle.value || lifecycle(p) === filterLifecycle.value)
}))

const deploymentsOf = (p) => deployments.value.filter(d => d.name === p.name)
const isDeleting = (p) => p.deleting || !!activeTaskFor(p.name, 'delete_project')

const tierIcon = (t) => ({ critical: '🔴', standard: '🟡', internal: '⚪' })[t] || '🟡'
const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const depIcon = (type) => ({ database: '🗄️', cache: '⚡', api: '🔌', queue: '📨' })[type] || '📦'
const linkIcon = (icon) => ({ monitoring: '📊', errors: '🐛', docs: '📖', code: '💻', ci: '⚙️', web: '🌐', docker: '🐳' })[icon] || '🔗'
const runtimeLabel = (rt) => ({ 'docker-local': '🐳 Docker', 'argocd-k3s': '☸️ Kubernetes' })[rt] || rt
const shortURL = (u) => u.replace(/^https?:\/\//, '')

// URL web du repo : la fiche la donne, sinon on la déduit de l'URL de clone
const repoWebURL = (p) => p.service?.spec.repo || p.repo_url?.replace(/\.git$/, '')
const pipelinesURL = (p) => p.service?.spec.links?.find(l => l.title === 'Pipelines')?.url
const ficheURL = (p) => {
  const base = repoWebURL(p)
  return base.includes('github.com') ? `${base}/blob/main/catalog-info.yaml` : `${base}/-/blob/main/catalog-info.yaml`
}

function select(p) {
  selectedName.value = selectedName.value === p.name ? null : p.name
}

async function load() {
  try {
    const [pr, dep] = await Promise.allSettled([api.listProjects(), api.listDeployments()])
    if (pr.status === 'fulfilled') projects.value = pr.value.data || []
    if (dep.status === 'fulfilled') deployments.value = dep.value.data || []
  } finally { loading.value = false }
}

// Fiches modifiées dans Git, déploiements qui changent d'état : relecture
// régulière, et à chaque pas d'une tâche (création, déploiement, suppression)
let timer
onMounted(() => {
  load()
  timer = setInterval(load, 15000)
})
onUnmounted(() => clearInterval(timer))
watch(() => activeTasks.value.map(t => `${t.id}:${t.step}`).join(), load)

async function trigger(action) {
  const inputs = {}
  action.inputs?.forEach(i => {
    const v = vals.value[action.name + i.id]
    if (v !== undefined && v !== '') inputs[i.id] = v
  })
  busy.value[action.name] = true
  feedback.value[action.name] = null
  try {
    // 202 : l'action est une tâche, suivie en direct jusqu'à la fin de sa pipeline
    const { data } = await api.triggerAction(selected.value.name, action.name, inputs)
    feedback.value[action.name] = { ok: true, taskId: trackTask(data) }
  } catch (e) {
    feedback.value[action.name] = { ok: false, msg: '❌ ' + (e.response?.data?.error || e.message) }
  } finally { busy.value[action.name] = false }
}

async function triggerPipeline(p) {
  try {
    const { data } = await api.triggerPipeline(p.repo_path, 'main', {})
    pipelines.value[p.repo_path] = { id: data.id, status: 'pending' }
    pollPipeline(p.repo_path, data.id)
  } catch (e) {
    alert('Erreur : ' + (e.response?.data?.error || e.message))
  }
}

function pollPipeline(path, id, attempts = 0) {
  if (attempts > 60) return
  setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(path, id)
      pipelines.value[path] = { id, status: data.status }
      if (!['success', 'failed', 'canceled'].includes(data.status)) pollPipeline(path, id, attempts + 1)
    } catch {}
  }, 5000)
}

async function deleteProject(p) {
  if (!confirm(`Supprimer définitivement ${p.name} ?\n\nSes déploiements (conteneurs, Application Argo CD, manifests), le repo ${p.repo_path}, ses images et sa fiche catalogue seront supprimés.`)) return
  try {
    const { data } = await api.deleteProject(p.name)
    trackTask(data)
  } catch (e) {
    alert('Erreur : ' + (e.response?.data?.error || e.message))
  }
}
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 18px; gap: 16px; }
h2 { font-size: 22px; font-weight: 700; margin-bottom: 4px; }
.subtitle { color: #888; font-size: 13px; }
.subtitle code, .small code { font-size: 12px; background: #f4f4f4; padding: 1px 5px; border-radius: 4px; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; text-decoration: none; white-space: nowrap; }
.btn-ghost { padding: 6px 12px; background: white; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #555; text-decoration: none; display: inline-block; }
.btn-ghost:hover { background: #f5f5f5; }
.btn-ghost.block { display: block; width: 100%; text-align: center; margin-top: 10px; }
.btn-danger { color: #c53030; border-color: #feb2b2; }
.btn-danger:hover { background: #fff5f5; }
.btn-danger:disabled { opacity: .6; cursor: wait; }
.btn-row { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.toolbar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 18px; }
.search { flex: 1; min-width: 200px; padding: 9px 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; }
.filter { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; cursor: pointer; }
.state { text-align: center; padding: 60px; color: #888; }
.state a { color: #667eea; text-decoration: none; font-weight: 500; margin-left: 6px; }
.empty { color: #888; text-align: center; padding: 40px; grid-column: 1/-1; font-size: 14px; }
.muted { color: #999; }
.small { font-size: 13px; line-height: 1.5; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; cursor: pointer; transition: all .15s; }
.card:hover { border-color: #667eea; transform: translateY(-1px); box-shadow: 0 4px 12px #667eea15; }
.card.active { border-color: #667eea; box-shadow: 0 0 0 2px #667eea33; }
.card-top { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 6px; gap: 8px; }
.card-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.card-desc { font-size: 13px; color: #777; margin-bottom: 10px; }
.svc-name { font-weight: 700; font-size: 15px; }
.tier-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; font-weight: 500; }
.tier-badge.critical { background: #fff5f5; color: #c53030; }
.tier-badge.standard { background: #fffbeb; color: #92400e; }
.tier-badge.internal { background: #f4f4f4; color: #666; }
.lifecycle-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; background: #f4f4f4; color: #666; }
.lifecycle-badge.production { background: #f0fff4; color: #276749; }
.lifecycle-badge.staging { background: #ebf8ff; color: #2b6cb0; }
.lifecycle-badge.experimental { background: #faf5ff; color: #6b46c1; }
.lifecycle-badge.deprecated { color: #999; text-decoration: line-through; }
.svc-meta { display: flex; gap: 10px; font-size: 13px; color: #666; margin-bottom: 10px; flex-wrap: wrap; }
.deploy-line { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 6px 10px; background: #f8f9fb; border-radius: 8px; margin-top: 6px; }
.deploy-line a { color: #667eea; text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.deploy-rt { color: #555; white-space: nowrap; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #cbd5e0; flex-shrink: 0; }
.dot.running { background: #38a169; }
.dot.deploying { background: #ecc94b; }
.dot.error { background: #e53e3e; }
.project-task { font-size: 12px; color: #4c51bf; background: #ebf4ff; border-radius: 6px; padding: 6px 10px; margin-top: 8px; }
.no-fiche { font-size: 12px; color: #92400e; background: #fffbeb; border-radius: 6px; padding: 6px 10px; margin-top: 8px; }
.tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.tag { font-size: 11px; background: #f4f4f4; padding: 2px 8px; border-radius: 20px; color: #555; }

/* Panneau */
.panel { position: fixed; right: 0; top: 56px; width: 420px; max-width: 100vw; height: calc(100vh - 56px); background: white; border-left: 1px solid #e2e2e2; padding: 24px; overflow-y: auto; box-shadow: -8px 0 24px #0001; z-index: 50; }
.panel-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 10px; }
.panel-header h3 { font-size: 20px; font-weight: 700; margin-bottom: 6px; }
.panel-badges { display: flex; gap: 6px; }
.panel-desc { font-size: 14px; color: #666; margin-bottom: 18px; }
.close { background: none; border: none; font-size: 20px; cursor: pointer; color: #aaa; }
section { margin-bottom: 20px; padding-bottom: 20px; border-bottom: 1px solid #f0f0f0; }
section:last-child { border-bottom: none; }
h4 { font-size: 11px; text-transform: uppercase; color: #aaa; letter-spacing: .08em; margin-bottom: 10px; font-weight: 600; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 5px 0; font-size: 14px; gap: 10px; }
.info-row span { color: #888; }
.info-row a { color: #667eea; text-decoration: none; font-size: 13px; }
.info-row code { font-size: 12px; background: #f4f4f4; padding: 2px 6px; border-radius: 4px; }
.pipeline-status { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; font-size: 13px; color: #666; padding: 8px 12px; background: #f8f9fb; border-radius: 8px; }
.status-badge { padding: 2px 10px; border-radius: 20px; font-size: 12px; font-weight: 500; background: #fffbeb; color: #92400e; }
.status-badge.success { background: #f0fff4; color: #276749; }
.status-badge.failed { background: #fff5f5; color: #c53030; }
.status-badge.running { background: #ebf8ff; color: #2b6cb0; }
.deploy-item { background: #f8f9fb; border-radius: 10px; padding: 12px; margin-bottom: 8px; font-size: 13px; }
.deploy-head { display: flex; justify-content: space-between; margin-bottom: 6px; }
.deploy-url { display: block; color: #667eea; text-decoration: none; font-weight: 500; margin-bottom: 4px; word-break: break-all; }
.deploy-image { font-size: 11px; color: #888; word-break: break-all; }
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
.action-desc { font-size: 12px; color: #888; margin: -6px 0 10px; }
.field { margin-bottom: 8px; }
.field label { display: block; font-size: 12px; color: #888; margin-bottom: 4px; font-weight: 500; }
.field input, .field select { width: 100%; padding: 8px 10px; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; background: white; }
.btn { width: 100%; padding: 9px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; margin-top: 6px; }
.btn:disabled { opacity: .5; cursor: not-allowed; }
.action-task { margin-top: 8px; }
.feedback { margin-top: 8px; padding: 8px 12px; border-radius: 6px; font-size: 13px; }
.feedback.ok { background: #f0fff4; color: #276749; }
.feedback.err { background: #fff5f5; color: #c53030; }
.slide-enter-active, .slide-leave-active { transition: transform .2s ease, opacity .2s; }
.slide-enter-from, .slide-leave-to { transform: translateX(20px); opacity: 0; }
</style>
