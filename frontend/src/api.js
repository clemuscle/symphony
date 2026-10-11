import axios from 'axios'

// L'API est sur la même origine que la page : servie par Symphony en production,
// relayée par le proxy de Vite en dev (vite.config.js). La session est un cookie
// HttpOnly posé par Symphony : le front ne manipule aucun jeton.
// VITE_API_URL (frontend/.env.local) permet de viser une autre instance.
const http = axios.create({ baseURL: import.meta.env.VITE_API_URL ?? '' })

// Connexion OIDC, puis retour sur la page courante
export function goToLogin() {
  window.location.href = '/auth/login?redirect=' + encodeURIComponent(location.pathname + location.search)
}

// Session absente ou expirée : direction la page de connexion
http.interceptors.response.use(undefined, (err) => {
  if (err.response?.status === 401) goToLogin()
  return Promise.reject(err)
})

export const api = {
  // Session
  me: () => http.get('/api/v1/me'),

  // Catalogue
  getServices: () => http.get('/api/v1/services'),
  triggerAction: (service, action, inputs) =>
    http.post(`/api/v1/services/${service}/actions/${action}`, inputs),

  // Golden Paths
  getGoldenPaths: () => http.get('/api/v1/golden-paths'),

  // Recharge le catalogue et les golden paths depuis Git
  sync: () => http.post('/api/v1/sync'),

  // Projets
  createProject: (data) => http.post('/api/v1/projects', data),
  listProjects: () => http.get('/api/v1/projects'),
  deleteProject: (name) => http.delete(`/api/v1/projects/${name}`),
  getProjectImage: (name) => http.get(`/api/v1/projects/${name}/image`),
  listRepos: () => http.get('/api/v1/repos'),

  // Pipelines
  triggerPipeline: (projectPath, ref, vars) =>
    http.post('/api/v1/pipelines/trigger', { repo_path: projectPath, ref, vars }),
  getPipelineStatus: (projectPath, pipelineID) =>
    http.get(`/api/v1/pipelines/status?repo=${projectPath}&id=${pipelineID}`),
  listPipelines: (project) => http.get(`/api/v1/pipelines/${project}`),

  // Déploiements
  listDeployments: () => http.get('/api/v1/deployments'),
  deploy: (data) => http.post('/api/v1/deployments', data),
  stopDeployment: (id) => http.delete(`/api/v1/deployments/${id}`),

  // Audit
  listAudit: () => http.get('/api/v1/audit'),
}
