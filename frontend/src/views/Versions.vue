<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Versions</h2>
        <p class="subtitle">Quelle version tourne dans quel environnement, ce qui attend une promotion ou une approbation, ce qui a échoué</p>
      </div>
      <button class="btn-refresh" @click="load()"><AppIcon name="refresh" /> Rafraîchir</button>
    </div>

    <div v-if="loading" class="state">Chargement...</div>
    <div v-else-if="error" class="state err"><AppIcon name="error" /> {{ error }}</div>
    <div class="table-wrap" v-else>
      <table class="matrix">
        <thead>
          <tr>
            <th>Projet</th>
            <th v-for="e in columns" :key="e.name">
              {{ e.name || 'déploiement' }}
              <AppIcon v-if="e.protected" name="lock" :size="12" title="Protégé : promotion seulement" />
              <AppIcon v-if="e.approval === 'lead'" name="approval" :size="12" title="Accord d'un lead pour un developer" />
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in projects" :key="p.name">
            <td class="project">
              <RouterLink :to="{ path: '/', query: { project: p.name } }">{{ p.name }}</RouterLink>
              <span class="team" v-if="p.team">{{ p.team }}</span>
            </td>
            <td v-for="(e, i) in columns" :key="e.name" :class="['cell', cellClass(p.name, e.name)]">
              <template v-if="release(p.name, e.name)">
                <div class="version">
                  <span :class="['dot', statusOf(p.name, e.name)]" :title="statusOf(p.name, e.name)"></span>
                  <code :title="releaseRef(release(p.name, e.name)!)">{{ shortTag(release(p.name, e.name)!.tag) }}</code>
                  <AppIcon v-if="release(p.name, e.name)!.origin !== 'deploy'" :name="release(p.name, e.name)!.origin === 'rollback' ? 'rollback' : 'promote'"
                    :size="12" :title="originLabels[release(p.name, e.name)!.origin]" />
                </div>
                <div class="meta">
                  {{ release(p.name, e.name)!.deployed_by }} · {{ ago(release(p.name, e.name)!.created_at, now) }}
                  <template v-if="release(p.name, e.name)!.approved_by"> · approuvé par {{ release(p.name, e.name)!.approved_by }}</template>
                </div>
              </template>
              <span class="none" v-else>—</span>
              <div class="chips">
                <span class="chip approval" v-if="pending(p.name, e.name)"><AppIcon name="approval" :size="12" /> à approuver</span>
                <span class="chip running" v-else-if="inProgress(p.name, e.name)"><AppIcon name="pending" :size="12" /> en cours</span>
                <span class="chip promote" v-if="i > 0 && behind(p.name, columns[i - 1].name, e.name)"
                  :title="'La version de ' + columns[i - 1].name + ' attend sa promotion'">
                  <AppIcon name="promote" :size="12" /> à promouvoir
                </span>
              </div>
            </td>
          </tr>
          <tr v-if="!projects.length"><td :colspan="columns.length + 1" class="none">Aucun projet</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, errorMessage } from '../api'
import AppIcon from '../components/AppIcon.vue'
import { ago } from '../lib/admin'
import { byProjectEnv, originLabels, releaseRef, shortTag } from '../lib/releases'
import { awaitingApproval, deployTarget } from '../lib/tasks'
import { activeTasks, allTasks } from '../stores/tasks'
import type { Deployment, Environment, Project, Release } from '../types'

const projects = ref<Project[]>([])
const environments = ref<Environment[]>([])
const releases = ref<Record<string, Record<string, Release>>>({})
const deployments = ref<Deployment[]>([])
const loading = ref(true)
const error = ref('')

// Sans environnements configurés : une seule colonne, le déploiement du projet
const columns = computed<Environment[]>(() =>
  environments.value.length ? environments.value : [{ name: '', runtime: '', protected: false }])

async function load() {
  error.value = ''
  try {
    const [p, e, r, d] = await Promise.all([
      api.listProjects(), api.listEnvironments(), api.listReleases({ latest: true, limit: 500 }), api.listDeployments(),
    ])
    projects.value = p.data ?? []
    environments.value = e.data ?? []
    releases.value = byProjectEnv(r.data ?? [])
    deployments.value = d.data ?? []
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

const release = (project: string, env: string): Release | undefined => releases.value[project]?.[env]
const live = (project: string, env: string) => deployments.value.find(d => d.project === project && d.environment === env)

// État affiché : celui du runtime si le déploiement existe, sinon celui de l'historique
function statusOf(project: string, env: string): string {
  const r = release(project, env)
  if (r?.status === 'failed') return 'error'
  return live(project, env)?.status ?? (r?.status === 'running' ? 'stopped' : r?.status ?? '')
}

const tasksIn = (project: string, env: string) =>
  activeTasks.value.filter(t => t.kind === 'deploy' && t.resource === project && (deployTarget(t).environment ?? '') === env)
const pending = (project: string, env: string) => tasksIn(project, env).some(awaitingApproval)
const inProgress = (project: string, env: string) => tasksIn(project, env).length > 0

// La version de l'environnement précédent, saine, n'est pas encore dans env
function behind(project: string, prev: string, env: string): boolean {
  const before = release(project, prev)
  const here = release(project, env)
  return !!before && before.status === 'running' && live(project, prev)?.status === 'running' &&
    !!before.tag && before.tag !== 'latest' && (!here || releaseRef(here) !== releaseRef(before))
}

function cellClass(project: string, env: string): string {
  if (pending(project, env)) return 'awaiting'
  if (release(project, env)?.status === 'failed') return 'failed'
  return ''
}

// Un déploiement qui se termine change la matrice : relecture
const finishedDeploys = computed(() =>
  allTasks.value.filter(t => t.kind === 'deploy' && (t.status === 'SUCCESS' || t.status === 'FAILED')).length)
watch(finishedDeploys, () => load())

const now = ref(new Date())
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  load()
  timer = setInterval(() => { now.value = new Date() }, 30000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; gap: 16px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.btn-refresh { padding: 8px 14px; background: white; border: 1px solid #e2e2e2; border-radius: 8px; cursor: pointer; font-size: 13px; display: inline-flex; gap: 6px; align-items: center; }
.state { color: #888; text-align: center; padding: 60px; }
.state.err { color: #c53030; }
.table-wrap { overflow-x: auto; background: white; border: 1px solid #e2e2e2; border-radius: 12px; }
.matrix { width: 100%; border-collapse: collapse; font-size: 13px; }
.matrix th { text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: #888; padding: 12px 16px; border-bottom: 1px solid #eee; white-space: nowrap; }
.matrix td { padding: 12px 16px; border-bottom: 1px solid #f2f2f2; vertical-align: top; }
.matrix tr:last-child td { border-bottom: none; }
.project a { color: #1a1a1a; font-weight: 600; text-decoration: none; }
.project a:hover { color: #667eea; }
.team { display: block; color: #999; font-size: 12px; margin-top: 2px; }
.cell.awaiting { background: #fffbeb; }
.cell.failed { background: #fff5f5; }
.version { display: flex; align-items: center; gap: 6px; }
.version code { font-size: 12px; background: #f4f4f8; padding: 2px 6px; border-radius: 4px; }
.meta { color: #999; font-size: 12px; margin-top: 4px; }
.none { color: #ccc; }
.chips { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 6px; }
.chip { display: inline-flex; align-items: center; gap: 4px; font-size: 11px; padding: 1px 8px; border-radius: 10px; }
.chip.approval { background: #fefcbf; color: #92400e; }
.chip.running { background: #ebf4ff; color: #4c51bf; }
.chip.promote { background: #f0f4ff; color: #667eea; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #cbd5e0; display: inline-block; }
.dot.running { background: #38a169; }
.dot.deploying { background: #d69e2e; }
.dot.error, .dot.exited, .dot.failed { background: #e53e3e; }
.dot.stopped { background: #a0aec0; }
</style>
