import axios from 'axios'

// URL de l'API Symphony, fixée au build par VITE_API_URL (frontend/.env.local par exemple).
// Une chaîne vide appelle la même origine que la page.
const http = axios.create({ baseURL: import.meta.env.VITE_API_URL ?? 'http://localhost:8080' })

export const api = {
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
