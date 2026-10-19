<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Activité</h2>
        <p class="subtitle">
          Créations, déploiements, approbations, suppressions et actions, en direct
          <span :class="['live', taskStore.connected ? 'on' : 'off']">
            {{ taskStore.connected ? '● en direct' : '○ reconnexion…' }}
          </span>
        </p>
      </div>
      <label class="filter"><input type="checkbox" v-model="onlyActive" /> en cours seulement</label>
    </div>

    <!-- À approuver : en tête, pour les leads de l'équipe et les admins -->
    <section class="to-approve" v-if="toApprove.length">
      <h3><AppIcon name="approval" /> À approuver</h3>
      <TaskProgress v-for="a in toApprove" :key="a.task.id" :task="taskStore.byId[a.task.id] ?? a.task" />
    </section>

    <div class="list">
      <TaskProgress v-for="t in shown" :key="t.id" :task="t" :compact="t.status === 'SUCCESS'" />
      <div class="empty" v-if="!shown.length">
        {{ onlyActive ? 'Aucune tâche en cours' : 'Aucune tâche pour l’instant' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import AppIcon from '../components/AppIcon.vue'
import TaskProgress from '../components/TaskProgress.vue'
import { taskStore, allTasks, activeTasks } from '../stores/tasks'
import { toApprove } from '../stores/approvals'

const onlyActive = ref(false)
// Les tâches à approuver sont déjà en tête
const pinned = computed(() => new Set(toApprove.value.map(a => a.task.id)))
const shown = computed(() => (onlyActive.value ? activeTasks.value : allTasks.value).filter(t => !pinned.value.has(t.id)))
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
.to-approve { display: flex; flex-direction: column; gap: 10px; margin-bottom: 24px; }
.to-approve h3 { font-size: 15px; display: flex; align-items: center; gap: 6px; color: #92400e; }
</style>
