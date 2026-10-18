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
      <input v-model="filter.search" placeholder="Rechercher (nom, équipe, langage, tag)..." class="search" />
      <select v-model="filter.tier" class="filter">
        <option value="">Tous les tiers</option>
        <option value="critical">Critical</option>
        <option value="standard">Standard</option>
        <option value="internal">Internal</option>
      </select>
      <select v-model="filter.lifecycle" class="filter">
        <option value="">Tous les cycles de vie</option>
        <option v-for="(label, value) in lifecycleLabels" :key="value" :value="value">{{ label }}</option>
      </select>
    </div>

    <div v-if="loading" class="state">Chargement...</div>
    <div v-else-if="!projects.length" class="state">
      Aucun projet pour l'instant.
      <RouterLink to="/catalogue">Choisir une stack dans le catalogue →</RouterLink>
    </div>

    <div class="grid" v-else>
      <ProjectCard v-for="p in filtered" :key="p.id" :project="p" :deployments="deploymentsOf(p, deployments)"
        :active="selectedName === p.name" @select="select(p)" />
      <div class="empty" v-if="!filtered.length">Aucun projet ne correspond aux filtres</div>
    </div>

    <!-- Panneau détail : le projet et sa fiche -->
    <Transition name="slide">
      <ProjectPanel v-if="selected" :key="selected.name" :project="selected"
        :deployments="deploymentsOf(selected, deployments)" @close="selectedName = null" />
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import ProjectCard from '../components/ProjectCard.vue'
import ProjectPanel from '../components/ProjectPanel.vue'
import { deploymentsOf, filterProjects, lifecycleLabels, type ProjectFilter } from '../lib/projects'
import { can } from '../stores/session'
import { activeTasks } from '../stores/tasks'
import type { Deployment, Project } from '../types'

const route = useRoute()
const projects = ref<Project[]>([])
const deployments = ref<Deployment[]>([])
const loading = ref(true)
const filter = reactive<ProjectFilter>({ search: '', tier: '', lifecycle: '' })
const selectedName = ref<string | null>(typeof route.query.project === 'string' ? route.query.project : null)

const selected = computed(() => projects.value.find(p => p.name === selectedName.value))
const filtered = computed(() => filterProjects(projects.value, filter))

function select(p: Project) {
  selectedName.value = selectedName.value === p.name ? null : p.name
}

async function load() {
  try {
    const [pr, dep] = await Promise.allSettled([api.listProjects(), api.listDeployments()])
    if (pr.status === 'fulfilled') projects.value = pr.value.data ?? []
    if (dep.status === 'fulfilled') deployments.value = dep.value.data ?? []
  } finally {
    loading.value = false
  }
}

// Fiches modifiées dans Git, déploiements qui changent d'état : relecture
// régulière, et à chaque pas d'une tâche (création, déploiement, suppression)
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  load()
  timer = setInterval(load, 15000)
})
onUnmounted(() => clearInterval(timer))
watch(() => activeTasks.value.map(t => `${t.id}:${t.step}`).join(), load)
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 18px; gap: 16px; }
h2 { font-size: 22px; font-weight: 700; margin-bottom: 4px; }
.subtitle { color: #888; font-size: 13px; }
.subtitle code, .small code { font-size: 12px; background: #f4f4f4; padding: 1px 5px; border-radius: 4px; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; text-decoration: none; white-space: nowrap; }
.toolbar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 18px; }
.search { flex: 1; min-width: 200px; padding: 9px 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; }
.filter { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; cursor: pointer; }
.state { text-align: center; padding: 60px; color: #888; }
.state a { color: #667eea; text-decoration: none; font-weight: 500; margin-left: 6px; }
.empty { color: #888; text-align: center; padding: 40px; grid-column: 1/-1; font-size: 14px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.slide-enter-active, .slide-leave-active { transition: transform .2s ease, opacity .2s; }
.slide-enter-from, .slide-leave-to { transform: translateX(20px); opacity: 0; }
</style>
