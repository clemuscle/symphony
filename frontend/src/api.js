import axios from 'axios'

const http = axios.create({
  baseURL: import.meta.env.VITE_API_URL || 'http://localhost:8080',
  withCredentials: true,
})

http.interceptors.response.use(
  r => r,
  err => {
    if (err.response?.status === 401 && !window.location.pathname.startsWith('/login')) {
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export const api = {
  // Catalogue
  getServices: () => http.get('/api/v1/services'),
  triggerAction: (service, action, inputs) =>
    http.post(`/api/v1/services/${service}/actions/${action}`, inputs),

  // Golden Paths
  getGoldenPaths: () => http.get('/api/v1/golden-paths'),
  reloadTemplates: () => http.post('/api/v1/templates/reload'),

  // Projets
  createProject: (data) => http.post('/api/v1/projects', data),
  listProjects: () => http.get('/api/v1/projects'),
  listProjectSteps: (name) => http.get(`/api/v1/projects/${encodeURIComponent(name)}/steps`),
  listRepos: () => http.get('/api/v1/repos'),
  listNamespaces: () => http.get('/api/v1/namespaces'),

  // Pipelines
  triggerPipeline: (projectPath, ref, vars) =>
    http.post('/api/v1/pipelines/trigger', { project_path: projectPath, ref, vars }),
  getPipelineStatus: (projectPath, pipelineID) =>
    http.get(`/api/v1/pipelines/status?project=${projectPath}&id=${pipelineID}`),
  listPipelines: (project) => http.get(`/api/v1/pipelines/${project}`),

  // Déploiements
  listDeployments: () => http.get('/api/v1/deployments'),
  deploy: (data) => http.post('/api/v1/deployments', data),
  stopDeployment: (id) => http.delete(`/api/v1/deployments/${id}`),

  // Recettes
  listRecettes: (projectName) => http.get(`/api/v1/projects/${encodeURIComponent(projectName)}/recettes`),
  createRecette: (projectName, data) => http.post(`/api/v1/projects/${encodeURIComponent(projectName)}/recettes`, data),
  destroyRecette: (projectName, recetteName) =>
    http.delete(`/api/v1/projects/${encodeURIComponent(projectName)}/recettes/${encodeURIComponent(recetteName)}`),

  // Inventaire
  getInventory: () => http.get('/api/v1/inventory'),

  // Coûts
  getCosts: (month) => http.get('/api/v1/costs' + (month ? `?month=${month}` : '')),

  // Audit
  listAudit: () => http.get('/api/v1/audit'),

  // Auth
  getMe: () => http.get('/api/v1/auth/me'),

  // Setup wizard
  getSetupStatus: () => http.get('/api/v1/setup/status'),
  testProvider: (type, config) => http.post('/api/v1/setup/test', { type, config }),
  saveSetup: (data) => http.post('/api/v1/setup/save', data),
  reloadConfig: () => http.post('/api/v1/config/reload'),
}
