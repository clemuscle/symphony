import { computed, reactive, watch } from 'vue'
import { api } from '../api'
import type { Approval } from '../types'
import { allTasks } from './tasks'
import { awaitingApproval } from '../lib/tasks'

// Déploiements en attente d'un lead (GET /api/v1/approvals), avec ce que
// l'utilisateur peut en faire. Relus quand l'ensemble des tâches en attente
// change (flux des tâches) : la cloche des leads reste à jour sans sondage.
export const approvals = reactive<{ list: Approval[] }>({ list: [] })

export async function refreshApprovals() {
  try {
    const { data } = await api.listApprovals()
    approvals.list = data ?? []
  } catch { /* gardé tel quel : la prochaine tâche relancera la lecture */ }
}

// Les ids des tâches qui attendent un lead, d'après le flux
const waitingIds = computed(() => allTasks.value.filter(awaitingApproval).map(t => t.id).join(','))

let watching = false
export function watchApprovals() {
  if (watching) return
  watching = true
  watch(waitingIds, refreshApprovals, { immediate: true })
}

// Ce que l'utilisateur doit approuver (la cloche)
export const toApprove = computed(() => approvals.list.filter(a => a.can_approve))

export const approvalFor = (taskId: number): Approval | undefined => approvals.list.find(a => a.task.id === taskId)
