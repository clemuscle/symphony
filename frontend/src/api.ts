import axios, { AxiosError } from 'axios'
import type {
  AdminStatus, AuditEntry, CreateProjectRequest, Deployment, DeployRequest, GoldenPath, IdentityGroups,
  Me, Pipeline, Project, ProjectImage, ProviderCheck, Task,
} from './types'
import { visitAdapter } from './visit/adapter'

// L'API est sur la même origine que la page : servie par Symphony en production,
// relayée par le proxy de Vite en dev (vite.config.js). La session est un cookie
// HttpOnly posé par Symphony : le front ne manipule aucun jeton.
// VITE_API_URL (frontend/.env.local) permet de viser une autre instance.
const http = axios.create({ baseURL: import.meta.env.VITE_API_URL ?? '' })

// Mode visite (GitHub Pages) : pas de serveur, l'API est servie par des données
// de démo, en lecture seule (src/visit)
// (constante à la compilation : hors mode visite, l'adaptateur et les données
// ne sont pas dans le bundle)
export const visitMode = import.meta.env.VITE_VISIT === '1'
if (visitMode) http.defaults.adapter = visitAdapter

// Connexion OIDC, puis retour sur la page courante
export function goToLogin() {
  if (visitMode) return
  window.location.href = '/auth/login?redirect=' + encodeURIComponent(location.pathname + location.search)
}

// Session absente ou expirée : direction la page de connexion
// (la requête reste alors en suspens : pas d'erreur affichée pendant que la page
// part vers Keycloak)
http.interceptors.response.use(undefined, (err: AxiosError) => {
  if (err.response?.status === 401 && !visitMode) {
    goToLogin()
    return new Promise(() => {})
  }
  return Promise.reject(err)
})

// Message d'erreur d'une réponse de l'API ({"error": "…"}), sinon celui d'axios
export function errorMessage(e: unknown): string {
  if (e instanceof AxiosError) {
    const data = e.response?.data as { error?: string } | undefined
    return data?.error || e.message
  }
  return e instanceof Error ? e.message : String(e)
}

export const api = {
  // Session
  me: () => http.get<Me>('/api/v1/me'),

  // Actions Day-2 déclarées dans la fiche catalogue d'un projet
  triggerAction: (service: string, action: string, inputs: Record<string, unknown>) =>
    http.post<Task>(`/api/v1/services/${service}/actions/${encodeURIComponent(action)}`, inputs),

  // Catalogue : les golden paths
  getGoldenPaths: () => http.get<GoldenPath[]>('/api/v1/golden-paths'),

  // Recharge le catalogue et les golden paths depuis Git
  sync: () => http.post('/api/v1/sync'),

  // Projets (chacun avec sa fiche catalogue, champ service)
  createProject: (data: CreateProjectRequest) => http.post<Task>('/api/v1/projects', data),
  listProjects: () => http.get<Project[]>('/api/v1/projects'),
  deleteProject: (name: string) => http.delete<Task>(`/api/v1/projects/${name}`),
  getProjectImage: (name: string) => http.get<ProjectImage>(`/api/v1/projects/${name}/image`),

  // Pipelines
  // Relance la pipeline d'un projet (son repo, son CI, sans variables)
  triggerPipeline: (project: string, ref: string) =>
    http.post<Pipeline>('/api/v1/pipelines/trigger', { project, ref }),
  getPipelineStatus: (repoPath: string, pipelineID: string) =>
    http.get<Pipeline>('/api/v1/pipelines/status', { params: { repo: repoPath, id: pipelineID } }),
  listPipelines: (repoPath: string) => http.get<Pipeline[]>(`/api/v1/pipelines/${repoPath}`),

  // Déploiements
  listDeployments: () => http.get<Deployment[]>('/api/v1/deployments'),
  deploy: (data: DeployRequest) => http.post<Task>('/api/v1/deployments', data),
  // Promotion : l'image saine de l'environnement précédent, déployée dans to
  promote: (project: string, to: string) => http.post<Task>(`/api/v1/projects/${project}/promote`, { to }),
  // provider : runtime qui porte le déploiement (champ provider de la liste)
  stopDeployment: (id: string, provider: string) =>
    http.delete(`/api/v1/deployments/${id}`, { params: { provider } }),
  restartDeployment: (id: string, provider: string) =>
    http.post(`/api/v1/deployments/${id}/restart`, null, { params: { provider } }),

  // Providers configurés (id par type)
  getProviders: () => http.get<Record<string, string[]>>('/api/v1/providers'),

  // Groupes de l'annuaire du provider d'identité (supported: false sans annuaire)
  listIdentityGroups: () => http.get<IdentityGroups>('/api/v1/identity/groups'),

  // Tâches (le flux en direct est dans stores/tasks.ts)
  listTasks: (params: { resource?: string; limit?: number } = {}) =>
    http.get<Task[]>('/api/v1/tasks', { params: { limit: 50, ...params } }),

  // Audit
  listAudit: () => http.get<AuditEntry[]>('/api/v1/audit'),

  // Administration : ce que Symphony a chargé ou refusé, et l'état des providers
  adminStatus: () => http.get<AdminStatus>('/api/v1/admin/status'),
  checkProviders: () => http.get<ProviderCheck[]>('/api/v1/admin/providers/check'),
}
