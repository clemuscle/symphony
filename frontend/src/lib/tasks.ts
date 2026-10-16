import type { Task } from '../types'

const kindLabels: Record<string, string> = {
  create_project: 'Création',
  deploy: 'Déploiement',
  delete_project: 'Suppression',
  action_webhook: 'Action',
  action_pipeline: 'Action (pipeline)',
}

// Libellé d'une tâche ; un déploiement dit son environnement, une promotion
// d'où elle part (résultat « target » posé par le serveur au début de la tâche)
export function taskTitle(task: Pick<Task, 'kind' | 'result'>): string {
  const label = kindLabels[task.kind] || task.kind
  const target = (task.result?.target ?? {}) as { environment?: string; promoted_from?: string }
  if (task.kind !== 'deploy' || !target.environment) return label
  if (target.promoted_from) return `Promotion ${target.promoted_from} → ${target.environment}`
  return `${label} en ${target.environment}`
}
