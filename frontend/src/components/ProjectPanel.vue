<template>
  <aside class="panel">
    <div class="panel-header">
      <div>
        <h3>{{ project.name }}</h3>
        <div class="panel-badges">
          <span :class="['tier-badge', tier(project)]">{{ tierIcon(tier(project)) }} {{ tier(project) }}</span>
          <span :class="['lifecycle-badge', lifecycle(project)]" v-if="lifecycle(project)" title="Cycle de vie (fiche catalogue)">{{ lifecycleLabel(lifecycle(project)) }}</span>
        </div>
      </div>
      <button class="close" @click="emit('close')">✕</button>
    </div>
    <p class="panel-desc" v-if="project.description">{{ project.description }}</p>

    <section>
      <h4>Code et CI</h4>
      <div class="btn-row">
        <a :href="repoWebURL(project)" target="_blank" class="btn-ghost">💻 Repo ↗</a>
        <a v-if="pipelinesURL(project)" :href="pipelinesURL(project)" target="_blank" class="btn-ghost">⚙️ Pipelines ↗</a>
        <!-- Avec une fiche, la relance passe par ses actions Day-2 -->
        <button class="btn-ghost" v-if="allowed && !svc" @click="triggerPipeline">▶ Relancer la pipeline</button>
      </div>
      <div class="pipeline-status" v-if="pipeline">
        <span>Pipeline #{{ pipeline.id }}</span>
        <span :class="['status-badge', pipeline.status]">{{ pipeline.status }}</span>
      </div>
      <div class="info-row"><span>Repo</span><code>{{ project.repo_path }}</code></div>
      <div class="info-row"><span>Golden path</span><code>{{ project.golden_path }}</code></div>
      <div class="info-row" v-if="project.team"><span>Équipe</span><strong>{{ project.team }}</strong></div>
      <div class="info-row" v-if="project.created_by"><span>Créé par</span><strong>{{ project.created_by }}</strong></div>
    </section>

    <!-- Environnements : un déploiement par environnement, promotion de l'un à l'autre -->
    <section v-if="project.environments.length">
      <h4>Environnements</h4>
      <div class="env-item" v-for="(e, i) in project.environments" :key="e.name">
        <div class="deploy-head">
          <span><strong>{{ e.name }}</strong> <span class="muted">· {{ runtimeLabel(e.runtime) }}</span>
            <span class="lock" v-if="e.protected" title="Protégé : promotion seulement">🔒</span></span>
          <span class="muted" v-if="byEnv(e.name)">
            <span :class="['dot', byEnv(e.name)!.status]"></span> {{ byEnv(e.name)!.status }}
          </span>
          <span class="muted" v-else>non déployé</span>
        </div>
        <template v-if="byEnv(e.name)">
          <a v-if="byEnv(e.name)!.url" :href="byEnv(e.name)!.url" target="_blank" class="deploy-url">{{ byEnv(e.name)!.url }} ↗</a>
          <code class="deploy-image">{{ byEnv(e.name)!.image }}</code>
        </template>
        <button class="btn-ghost block" v-if="i > 0 && allowed"
          :disabled="!canPromote(i) || promoting"
          :title="canPromote(i) ? '' : 'Rien de sain à promouvoir en ' + project.environments[i - 1].name"
          @click="promote(e.name)">
          ⬆ Promouvoir depuis {{ project.environments[i - 1].name }}
        </button>
      </div>
      <TaskProgress v-if="promoteTask" :task="promoteTask" />
      <div class="feedback err" v-if="promoteError">❌ {{ promoteError }}</div>
      <RouterLink v-if="allowed" :to="{ path: '/deployments', query: { project: project.name } }" class="btn-ghost block">
        🚀 Déployer une version
      </RouterLink>
      <div class="muted small" v-else-if="can('developer')">
        Déploiement et actions réservés à l'équipe {{ project.team || 'propriétaire' }} et aux admins.
      </div>
    </section>

    <section v-else>
      <h4>Déploiements</h4>
      <div class="deploy-item" v-for="d in deployments" :key="d.provider + d.id">
        <div class="deploy-head">
          <span><span :class="['dot', d.status]"></span> {{ runtimeLabel(d.provider) }}</span>
          <span class="muted">{{ d.status }}<template v-if="d.sync"> · {{ d.sync }} / {{ d.health }}</template></span>
        </div>
        <a v-if="d.url" :href="d.url" target="_blank" class="deploy-url">{{ d.url }} ↗</a>
        <code class="deploy-image">{{ d.image }}</code>
      </div>
      <div class="muted small" v-if="!deployments.length">Aucun déploiement.</div>
      <RouterLink v-if="allowed" :to="{ path: '/deployments', query: { project: project.name } }" class="btn-ghost block">
        🚀 Déployer une version
      </RouterLink>
      <div class="muted small" v-else-if="can('developer')">
        Déploiement et actions réservés à l'équipe {{ project.team || 'propriétaire' }} et aux admins.
      </div>
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
        <DayTwoAction v-for="action in svc.spec.actions" :key="action.name"
          :action="action" :project="project.name" :allowed="allowed" />
      </section>
    </template>

    <section>
      <h4>Fiche catalogue</h4>
      <p class="muted small">
        Les métadonnées (équipe, tier, SLO, liens, actions) viennent de
        <code>catalog-info.yaml</code> dans le repo : l'équipe les modifie par un commit.
      </p>
      <a :href="ficheURL(project)" target="_blank" class="btn-ghost block">✏️ Modifier catalog-info.yaml ↗</a>
    </section>

    <section v-if="can('admin')">
      <h4>Zone dangereuse</h4>
      <button class="btn-ghost btn-danger block" :disabled="deleting" @click="deleteProject">
        {{ deleting ? 'Suppression…' : '🗑 Supprimer le projet' }}
      </button>
    </section>
  </aside>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { api, errorMessage } from '../api'
import { depIcon, ficheURL, lifecycle, lifecycleLabel, linkIcon, pipelinesURL, repoWebURL, runtimeLabel, tier, tierIcon } from '../lib/projects'
import { can, canOperate } from '../stores/session'
import { activeTaskFor, taskStore, trackTask } from '../stores/tasks'
import type { Deployment, Project } from '../types'
import DayTwoAction from './DayTwoAction.vue'
import TaskProgress from './TaskProgress.vue'

const props = defineProps<{ project: Project; deployments: Deployment[] }>()
const emit = defineEmits<{ close: [] }>()

const svc = computed(() => props.project.service)
const allowed = computed(() => canOperate(props.project.team))
const deleting = computed(() => !!props.project.deleting || !!activeTaskFor(props.project.name, 'delete_project'))
// Repo et Pipelines ont déjà leurs boutons
const extraLinks = computed(() =>
  (svc.value?.spec.links ?? []).filter(l => !['Repository', 'Pipelines'].includes(l.title)))

// Environnements : le déploiement de chacun, et la promotion depuis le précédent
const byEnv = (env: string) => props.deployments.find(d => d.environment === env)
const canPromote = (i: number) => byEnv(props.project.environments[i - 1].name)?.status === 'running'
const promoting = ref(false)
const promoteError = ref('')
const promoteTaskId = ref<number | null>(null)
const promoteTask = computed(() => (promoteTaskId.value ? taskStore.byId[promoteTaskId.value] : undefined))

async function promote(to: string) {
  promoting.value = true
  promoteError.value = ''
  try {
    const { data } = await api.promote(props.project.name, to)
    promoteTaskId.value = trackTask(data)
  } catch (e) {
    promoteError.value = errorMessage(e)
  } finally {
    promoting.value = false
  }
}

// Relance manuelle de la pipeline (projet sans fiche) : suivie par sondage
const pipeline = ref<{ id: string; status: string } | null>(null)
let pollTimer: ReturnType<typeof setTimeout> | undefined
onUnmounted(() => clearTimeout(pollTimer))

async function triggerPipeline() {
  try {
    const { data } = await api.triggerPipeline(props.project.name, 'main')
    pipeline.value = { id: data.id, status: 'pending' }
    pollPipeline(data.id)
  } catch (e) {
    alert('Erreur : ' + errorMessage(e))
  }
}

function pollPipeline(id: string, attempts = 0) {
  if (attempts > 60) return
  pollTimer = setTimeout(async () => {
    try {
      const { data } = await api.getPipelineStatus(props.project.repo_path, id)
      pipeline.value = { id, status: data.status }
      if (!['success', 'failed', 'canceled'].includes(data.status)) pollPipeline(id, attempts + 1)
    } catch { /* réessayé au prochain tour */ }
  }, 5000)
}

async function deleteProject() {
  const p = props.project
  if (!confirm(`Supprimer définitivement ${p.name} ?\n\nSes déploiements (conteneurs, Application Argo CD, manifests), le repo ${p.repo_path}, ses images et sa fiche catalogue seront supprimés.`)) return
  try {
    const { data } = await api.deleteProject(p.name)
    trackTask(data)
  } catch (e) {
    alert('Erreur : ' + errorMessage(e))
  }
}
</script>

<style scoped>
.panel { position: fixed; right: 0; top: 56px; width: 420px; max-width: 100vw; height: calc(100vh - 56px); background: white; border-left: 1px solid #e2e2e2; padding: 24px; overflow-y: auto; box-shadow: -8px 0 24px #0001; z-index: 50; }
.panel-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 10px; }
.panel-header h3 { font-size: 20px; font-weight: 700; margin-bottom: 6px; }
.panel-badges { display: flex; gap: 6px; }
.panel-desc { font-size: 14px; color: #666; margin-bottom: 18px; }
.close { background: none; border: none; font-size: 20px; cursor: pointer; color: #aaa; }
section { margin-bottom: 20px; padding-bottom: 20px; border-bottom: 1px solid #f0f0f0; }
section:last-child { border-bottom: none; }
h4 { font-size: 11px; text-transform: uppercase; color: #aaa; letter-spacing: .08em; margin-bottom: 10px; font-weight: 600; }
.btn-ghost { padding: 6px 12px; background: white; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #555; text-decoration: none; display: inline-block; }
.btn-ghost:hover { background: #f5f5f5; }
.btn-ghost.block { display: block; width: 100%; text-align: center; margin-top: 10px; }
.btn-danger { color: #c53030; border-color: #feb2b2; }
.btn-danger:hover { background: #fff5f5; }
.btn-danger:disabled { opacity: .6; cursor: wait; }
.btn-row { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.muted { color: #999; }
.small { font-size: 13px; line-height: 1.5; }
.small code { font-size: 12px; background: #f4f4f4; padding: 1px 5px; border-radius: 4px; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 5px 0; font-size: 14px; gap: 10px; }
.info-row span { color: #888; }
.info-row a { color: #667eea; text-decoration: none; font-size: 13px; }
.info-row code { font-size: 12px; background: #f4f4f4; padding: 2px 6px; border-radius: 4px; }
.pipeline-status { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; font-size: 13px; color: #666; padding: 8px 12px; background: #f8f9fb; border-radius: 8px; }
.status-badge { padding: 2px 10px; border-radius: 20px; font-size: 12px; font-weight: 500; background: #fffbeb; color: #92400e; }
.status-badge.success { background: #f0fff4; color: #276749; }
.status-badge.failed { background: #fff5f5; color: #c53030; }
.status-badge.running { background: #ebf8ff; color: #2b6cb0; }
.env-item { background: #f8f9fb; border-radius: 10px; padding: 12px; margin-bottom: 8px; font-size: 13px; }
.lock { margin-left: 4px; }
.feedback { margin-top: 8px; padding: 8px 12px; border-radius: 6px; font-size: 13px; }
.feedback.err { background: #fff5f5; color: #c53030; }
.deploy-item { background: #f8f9fb; border-radius: 10px; padding: 12px; margin-bottom: 8px; font-size: 13px; }
.deploy-head { display: flex; justify-content: space-between; margin-bottom: 6px; }
.deploy-url { display: block; color: #667eea; text-decoration: none; font-weight: 500; margin-bottom: 4px; word-break: break-all; }
.deploy-image { font-size: 11px; color: #888; word-break: break-all; }
.tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.tag { font-size: 11px; background: #f4f4f4; padding: 2px 8px; border-radius: 20px; color: #555; }
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
</style>
