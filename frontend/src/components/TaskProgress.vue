<template>
  <div class="task" :class="task.status">
    <div class="task-head">
      <span class="task-title">
        {{ taskTitle(task) }} · <strong>{{ task.resource }}</strong>
        <span class="task-id">#{{ task.id }}</span>
      </span>
      <span :class="['task-status', task.status]">{{ statusLabel }}</span>
    </div>

    <ol class="steps" v-if="!compact || !done">
      <li v-for="(label, i) in task.steps" :key="i" :class="stepState(i)">
        <span class="icon">{{ icons[stepState(i)] }}</span>
        <span class="label">{{ label }}</span>
        <span class="detail" v-if="i === task.step && task.detail && !done">— {{ task.detail }}</span>
      </li>
    </ol>

    <div class="error" v-if="task.error">❌ {{ task.error }}</div>

    <div class="links" v-if="links.length">
      <a v-for="l in links" :key="l.url" :href="l.url" target="_blank">{{ l.label }} ↗</a>
    </div>

    <div class="meta">
      {{ task.created_by }} · {{ when }}<template v-if="duration"> · {{ duration }}</template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { taskTitle } from '../lib/tasks'
import type { Task } from '../types'

const props = withDefaults(defineProps<{
  task: Task
  compact?: boolean // tâche finie : une ligne, sans les étapes
}>(), { compact: false })

type StepState = 'done' | 'current' | 'waiting' | 'failed'
const icons: Record<StepState, string> = { done: '✓', current: '◐', waiting: '○', failed: '✗' }
const done = computed(() => props.task.status === 'SUCCESS' || props.task.status === 'FAILED')

const statusLabels: Record<string, string> = {
  PENDING: 'en file', RUNNING: 'en cours', SUCCESS: 'terminée', FAILED: 'échouée',
}
const statusLabel = computed(() => statusLabels[props.task.status] || props.task.status)

function stepState(i: number): StepState {
  const t = props.task
  if (t.status === 'SUCCESS' || i < t.step) return 'done'
  if (i === t.step) return t.status === 'FAILED' ? 'failed' : (t.status === 'RUNNING' ? 'current' : 'waiting')
  return 'waiting'
}

// Liens utiles selon le type de tâche : repo, pipeline en cours, service déployé
// Résultats des étapes utiles à l'affichage (le reste n'est pas lu)
interface TaskResult {
  repo?: { web_url?: string }
  pipeline?: { web_url?: string }
  pipelines_url?: string
  deployment?: { url?: string }
}

const links = computed(() => {
  const r = (props.task.result ?? {}) as TaskResult
  const out: { label: string; url: string }[] = []
  if (r.repo?.web_url) out.push({ label: 'Repo', url: r.repo.web_url })
  const pipeline = props.task.wait?.web_url || r.pipeline?.web_url || r.pipelines_url
  if (pipeline) out.push({ label: 'Pipeline', url: pipeline })
  if (r.deployment?.url) out.push({ label: 'Service', url: r.deployment.url })
  return out
})

// Durée rafraîchie chaque seconde tant que la tâche tourne
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => clearInterval(timer))

const when = computed(() => new Date(props.task.created_at).toLocaleTimeString('fr-FR'))
const duration = computed(() => {
  const end = props.task.finished_at ? new Date(props.task.finished_at).getTime() : now.value
  const s = Math.max(0, Math.round((end - new Date(props.task.created_at).getTime()) / 1000))
  return s < 60 ? `${s} s` : `${Math.floor(s / 60)} min ${String(s % 60).padStart(2, '0')} s`
})
</script>

<style scoped>
.task { background: white; border: 1px solid #e2e2e2; border-left: 4px solid #cbd5e0; border-radius: 10px; padding: 14px 16px; font-size: 13px; }
.task.RUNNING, .task.PENDING { border-left-color: #667eea; }
.task.SUCCESS { border-left-color: #38a169; }
.task.FAILED { border-left-color: #e53e3e; }
.task-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 8px; }
.task-title { font-size: 14px; }
.task-id { color: #aaa; font-size: 12px; margin-left: 4px; }
.task-status { font-size: 12px; padding: 2px 10px; border-radius: 20px; background: #f4f4f8; color: #555; white-space: nowrap; }
.task-status.RUNNING, .task-status.PENDING { background: #ebf4ff; color: #4c51bf; }
.task-status.SUCCESS { background: #f0fff4; color: #276749; }
.task-status.FAILED { background: #fff5f5; color: #c53030; }
.steps { list-style: none; display: flex; flex-direction: column; gap: 4px; margin: 6px 0; }
.steps li { display: flex; gap: 8px; align-items: baseline; color: #999; }
.steps li.done { color: #276749; }
.steps li.current { color: #1a1a1a; font-weight: 500; }
.steps li.failed { color: #c53030; }
.icon { width: 14px; text-align: center; }
.steps li.current .icon { display: inline-block; animation: spin 1.2s linear infinite; }
.detail { color: #666; font-weight: 400; }
.error { margin-top: 6px; color: #c53030; }
.links { display: flex; gap: 12px; margin-top: 8px; }
.links a { color: #667eea; text-decoration: none; }
.meta { margin-top: 6px; color: #aaa; font-size: 12px; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
