<template>
  <div class="card" :class="{ active }" @click="emit('select')">
    <div class="card-top">
      <div class="card-name-row">
        <span class="svc-name">{{ project.name }}</span>
        <span :class="['tier-badge', tier(project)]">{{ tierIcon(tier(project)) }} {{ tier(project) }}</span>
      </div>
      <span :class="['lifecycle-badge', lifecycle(project)]" v-if="lifecycle(project)" title="Cycle de vie (fiche catalogue)">{{ lifecycleLabel(lifecycle(project)) }}</span>
    </div>
    <div class="card-desc">{{ project.description || 'Pas de description' }}</div>
    <div class="svc-meta">
      <span v-if="owner(project)">👤 {{ owner(project) }}</span>
      <span>{{ langIcon(project.language) }} {{ project.language }}</span>
      <span class="muted">{{ project.stack }}</span>
    </div>

    <div class="deploy-line" v-for="d in deployments" :key="d.provider + d.id">
      <span :class="['dot', d.status]"></span>
      <span class="deploy-rt">{{ runtimeLabel(d.provider) }}</span>
      <a v-if="d.url && d.status === 'running'" :href="d.url" target="_blank" @click.stop>{{ shortURL(d.url) }} ↗</a>
      <span v-else class="muted">{{ d.status }}</span>
    </div>
    <div class="deploy-line muted" v-if="!deployments.length && !task">Pas encore déployé</div>

    <div class="project-task" v-if="task">
      ⚡ {{ taskTitle(task) }} : {{ task.steps[task.step] }}
    </div>
    <div class="no-fiche" v-else-if="!project.service">Fiche catalogue absente du repo</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { lifecycle, lifecycleLabel, langIcon, owner, runtimeLabel, shortURL, tier, tierIcon } from '../lib/projects'
import { taskTitle } from '../lib/tasks'
import { activeTaskFor } from '../stores/tasks'
import type { Deployment, Project } from '../types'

const props = defineProps<{ project: Project; deployments: Deployment[]; active: boolean }>()
const emit = defineEmits<{ select: [] }>()

// Tâche en cours sur le projet (création, déploiement, suppression…)
const task = computed(() => activeTaskFor(props.project.name))
</script>

<style scoped>
.card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 18px; cursor: pointer; transition: all .15s; }
.card:hover { border-color: #667eea; transform: translateY(-1px); box-shadow: 0 4px 12px #667eea15; }
.card.active { border-color: #667eea; box-shadow: 0 0 0 2px #667eea33; }
.card-top { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 6px; gap: 8px; }
.card-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.card-desc { font-size: 13px; color: #777; margin-bottom: 10px; }
.svc-name { font-weight: 700; font-size: 15px; }
.svc-meta { display: flex; gap: 10px; font-size: 13px; color: #666; margin-bottom: 10px; flex-wrap: wrap; }
.deploy-line { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 6px 10px; background: #f8f9fb; border-radius: 8px; margin-top: 6px; }
.deploy-line a { color: #667eea; text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.deploy-rt { color: #555; white-space: nowrap; }
.project-task { font-size: 12px; color: #4c51bf; background: #ebf4ff; border-radius: 6px; padding: 6px 10px; margin-top: 8px; }
.no-fiche { font-size: 12px; color: #92400e; background: #fffbeb; border-radius: 6px; padding: 6px 10px; margin-top: 8px; }
.muted { color: #999; }
</style>
