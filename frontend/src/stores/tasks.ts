import { reactive, computed } from 'vue'
import { api, visitMode } from '../api'
import type { Task } from '../types'

// Tâches de Symphony (création, déploiement, suppression, actions), tenues à
// jour en direct : une connexion Server-Sent Events par onglet, ouverte une
// fois la session chargée. À chaque (re)connexion, la liste est relue : rien
// n'est perdu pendant une coupure.
export const taskStore = reactive<{ byId: Record<number, Task>; connected: boolean }>({
  byId: {},
  connected: false,
})

let source: EventSource | null = null

export function connectTasks() {
  if (visitMode) {
    // Pas de flux en mode visite : la liste capturée, une fois
    api.listTasks().then(({ data }) => { for (const t of data ?? []) upsert(t) }).catch(() => {})
    return
  }
  if (source) return
  const base = import.meta.env.VITE_API_URL ?? ''
  source = new EventSource(base + '/api/v1/tasks/events', { withCredentials: true })
  source.onopen = async () => {
    taskStore.connected = true
    try {
      const { data } = await api.listTasks()
      for (const t of data || []) upsert(t)
    } catch { /* la session a peut-être expiré : l'intercepteur s'en charge */ }
  }
  source.onerror = () => { taskStore.connected = false } // EventSource se reconnecte seul
  source.addEventListener('task', (e) => upsert(JSON.parse((e as MessageEvent).data) as Task))
}

function upsert(t: Task) {
  const prev = taskStore.byId[t.id]
  // Un événement ancien arrivé après la relecture de la liste ne recule pas l'état
  if (prev && new Date(prev.updated_at) > new Date(t.updated_at)) return
  taskStore.byId[t.id] = t
}

// Une tâche reçue de l'API (réponse 202) : affichée tout de suite
export function trackTask(t: Task): number {
  upsert(t)
  return t.id
}

export const allTasks = computed(() =>
  Object.values(taskStore.byId).sort((a, b) => b.id - a.id))

export const activeTasks = computed(() =>
  allTasks.value.filter(t => t.status === 'PENDING' || t.status === 'RUNNING'))

// Tâche non terminée sur une ressource (projet, service), sinon undefined
export function activeTaskFor(resource: string, kind?: string): Task | undefined {
  return activeTasks.value.find(t => t.resource === resource && (!kind || t.kind === kind))
}
