import type { Decision, DeployTarget, Task } from '../types'

const kindLabels: Record<string, string> = {
  create_project: 'Création',
  deploy: 'Déploiement',
  delete_project: 'Suppression',
  action_webhook: 'Action',
  action_pipeline: 'Action (pipeline)',
}

// Cible d'un déploiement (résultat « target » posé par le serveur dès la
// première étape) et décision d'un lead (résultat « approval »)
export const deployTarget = (task: Pick<Task, 'result'>): DeployTarget => (task.result?.target ?? {}) as DeployTarget
export const decision = (task: Pick<Task, 'result'>): Decision | undefined =>
  task.result?.approval as Decision | undefined

// Libellé d'une tâche ; un déploiement dit son environnement, une promotion
// d'où elle part, un retour arrière qu'il en est un
export function taskTitle(task: Pick<Task, 'kind' | 'result'>): string {
  const label = kindLabels[task.kind] || task.kind
  const target = deployTarget(task)
  if (task.kind !== 'deploy' || !target.environment) {
    return task.kind === 'deploy' && target.origin === 'rollback' ? 'Retour arrière' : label
  }
  if (target.promoted_from) return `Promotion ${target.promoted_from} → ${target.environment}`
  if (target.origin === 'rollback') return `Retour arrière en ${target.environment}`
  return `${label} en ${target.environment}`
}

// La tâche attend l'accord d'un lead
export const awaitingApproval = (task: Pick<Task, 'status' | 'wait'>): boolean =>
  !!task.wait?.approval && (task.status === 'PENDING' || task.status === 'RUNNING')
