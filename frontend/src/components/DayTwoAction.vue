<template>
  <div class="action-block">
    <div class="action-title"><AppIcon name="action" /> {{ action.name }}</div>
    <div class="action-desc" v-if="action.description">{{ action.description }}</div>
    <div v-for="input in action.inputs ?? []" :key="input.id" class="field">
      <label>{{ input.label || input.id }}</label>
      <select v-if="input.options?.length" v-model="values[input.id]">
        <option v-for="o in input.options" :key="o" :value="o">{{ o }}</option>
      </select>
      <input v-else-if="input.type === 'integer'" type="number" :min="input.min" :max="input.max"
        v-model="values[input.id]" :placeholder="input.default" />
      <input v-else type="text" v-model="values[input.id]" :placeholder="input.default || input.id" />
    </div>
    <button class="btn" v-if="allowed" :disabled="busy" @click="trigger">
      {{ busy ? 'Envoi...' : 'Déclencher' }}
    </button>
    <TaskProgress class="action-task" v-if="task" :task="task" />
    <div v-else-if="error" class="feedback err"><AppIcon name="error" /> {{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, errorMessage } from '../api'
import { taskStore, trackTask } from '../stores/tasks'
import type { Action } from '../types'
import AppIcon from './AppIcon.vue'
import TaskProgress from './TaskProgress.vue'

const props = defineProps<{
  action: Action
  project: string
  allowed: boolean // l'utilisateur peut agir sur le projet (son équipe, ou admin)
}>()

const values = ref<Record<string, string | number>>({})
const busy = ref(false)
const error = ref('')
const taskId = ref<number | null>(null)
const task = computed(() => (taskId.value ? taskStore.byId[taskId.value] : undefined))

// Seules les entrées renseignées partent : le serveur applique les défauts et
// refuse toute entrée non déclarée
async function trigger() {
  const inputs: Record<string, string | number> = {}
  for (const i of props.action.inputs ?? []) {
    const v = values.value[i.id]
    if (v !== undefined && v !== '') inputs[i.id] = v
  }
  busy.value = true
  error.value = ''
  taskId.value = null
  try {
    // 202 : l'action est une tâche, suivie en direct jusqu'à la fin de sa pipeline
    const { data } = await api.triggerAction(props.project, props.action.name, inputs)
    taskId.value = trackTask(data)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
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
.feedback.err { background: #fff5f5; color: #c53030; }
</style>
