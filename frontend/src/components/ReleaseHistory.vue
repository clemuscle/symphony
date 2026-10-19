<template>
  <div class="history">
    <button class="btn-ghost block" v-if="allowed && target" :disabled="busy" @click="rollback(target)"
      :title="'Redéployer ' + releaseRef(target) + (needsApproval ? ' (accord d\'un lead)' : '')">
      <AppIcon name="rollback" /> Revenir à {{ shortTag(target.tag) }}
    </button>
    <details v-if="history.length">
      <summary>Historique ({{ history.length }})</summary>
      <ol>
        <li v-for="r in history.slice(0, 10)" :key="r.id" :class="{ current: r.id === history[0].id }">
          <span :class="['dot', r.status === 'failed' ? 'error' : r.status]" :title="r.status"></span>
          <code :title="releaseRef(r)">{{ shortTag(r.tag) }}</code>
          <span class="muted">{{ originLabels[r.origin] }} · {{ r.deployed_by }} · {{ ago(r.created_at) }}<template v-if="r.approved_by"> · approuvé par {{ r.approved_by }}</template></span>
          <button class="link" v-if="allowed && r.id !== history[0].id && ranOnce(r) && r.tag && r.tag !== 'latest'"
            :disabled="busy" @click="rollback(r)">revenir</button>
        </li>
      </ol>
    </details>
    <TaskProgress v-if="task" :task="task" />
    <div class="feedback err" v-if="error"><AppIcon name="error" /> {{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, errorMessage } from '../api'
import { ago } from '../lib/admin'
import { originLabels, ranOnce, releaseRef, rollbackTarget, shortTag } from '../lib/releases'
import { taskStore, trackTask } from '../stores/tasks'
import type { Release } from '../types'
import AppIcon from './AppIcon.vue'
import TaskProgress from './TaskProgress.vue'

const props = defineProps<{
  project: string
  environment: string // vide : sans environnements
  releases: Release[] // historique du projet, tous environnements
  current?: string // image en place (runtime)
  allowed: boolean
  needsApproval?: boolean
}>()

const history = computed(() => props.releases.filter(r => r.environment === props.environment).sort((a, b) => b.id - a.id))
const target = computed(() => rollbackTarget(history.value, props.current))

const busy = ref(false)
const error = ref('')
const taskId = ref<number | null>(null)
const task = computed(() => (taskId.value ? taskStore.byId[taskId.value] : undefined))

async function rollback(r: Release) {
  const where = props.environment ? ` en ${props.environment}` : ''
  const approval = props.needsApproval ? "\n\nUn lead de l'équipe devra l'approuver." : ''
  if (!confirm(`Revenir à ${releaseRef(r)}${where} ?${approval}`)) return
  busy.value = true
  error.value = ''
  try {
    const { data } = await api.rollback(props.project, props.environment, r.id)
    taskId.value = trackTask(data)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.history { margin-top: 6px; }
details { margin-top: 8px; font-size: 12px; }
summary { cursor: pointer; color: #888; }
ol { list-style: none; margin-top: 6px; display: flex; flex-direction: column; gap: 4px; }
li { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
li.current code { font-weight: 700; }
code { font-size: 11px; background: #fff; padding: 1px 5px; border-radius: 4px; }
.muted { color: #999; }
.link { background: none; border: none; color: #667eea; cursor: pointer; font-size: 12px; padding: 0; }
.link:disabled { opacity: .5; cursor: wait; }
.btn-ghost { padding: 6px 12px; background: white; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #555; }
.btn-ghost:hover { background: #f5f5f5; }
.btn-ghost.block { display: block; width: 100%; text-align: center; margin-top: 8px; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #cbd5e0; display: inline-block; }
.dot.running { background: #38a169; }
.dot.deploying { background: #d69e2e; }
.dot.error { background: #e53e3e; }
.dot.stopped { background: #a0aec0; }
.feedback { margin-top: 8px; padding: 8px 12px; border-radius: 6px; font-size: 13px; }
.feedback.err { background: #fff5f5; color: #c53030; }
</style>
