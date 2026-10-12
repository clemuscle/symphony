<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Activité</h2>
        <p class="subtitle">
          Créations, déploiements, suppressions et actions, en direct
          <span :class="['live', taskStore.connected ? 'on' : 'off']">
            {{ taskStore.connected ? '● en direct' : '○ reconnexion…' }}
          </span>
        </p>
      </div>
      <label class="filter"><input type="checkbox" v-model="onlyActive" /> en cours seulement</label>
    </div>

    <div class="list">
      <TaskProgress v-for="t in shown" :key="t.id" :task="t" :compact="t.status === 'SUCCESS'" />
      <div class="empty" v-if="!shown.length">
        {{ onlyActive ? 'Aucune tâche en cours' : 'Aucune tâche pour l’instant' }}
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import TaskProgress from '../components/TaskProgress.vue'
import { taskStore, allTasks, activeTasks } from '../stores/tasks'

const onlyActive = ref(false)
const shown = computed(() => onlyActive.value ? activeTasks.value : allTasks.value)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.live { margin-left: 10px; font-size: 12px; }
.live.on { color: #38a169; }
.live.off { color: #d69e2e; }
.filter { font-size: 13px; color: #555; display: flex; gap: 6px; align-items: center; }
.list { display: flex; flex-direction: column; gap: 10px; }
.empty { color: #888; text-align: center; padding: 60px; }
</style>
