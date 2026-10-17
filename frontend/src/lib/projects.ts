// Fonctions pures autour des projets et de leur fiche (testées dans
// projects.test.ts)
import type { Deployment, Project } from '../types'

export const tier = (p: Project): string => p.service?.metadata.tier || 'standard'
export const lifecycle = (p: Project): string => p.service?.metadata.lifecycle || ''

// Cycle de vie (maturité déclarée dans la fiche), en mots qui ne se confondent
// pas avec les environnements où le projet est déployé
export const lifecycleLabels: Record<string, string> = {
  experimental: '🧪 expérimental', staging: '🚧 en rodage', production: '✅ en service', deprecated: '⛔ obsolète',
}
export const lifecycleLabel = (value: string): string => lifecycleLabels[value] || value
export const owner = (p: Project): string => p.service?.metadata.owner || p.team || ''

const lookup = (table: Record<string, string>, fallback: string) =>
  (key: string | undefined): string => (key && table[key]) || fallback

export const tierIcon = lookup({ critical: '🔴', standard: '🟡', internal: '⚪' }, '🟡')
export const langIcon = lookup({ go: '🐹', python: '🐍', node: '💚', java: '☕' }, '📦')
export const depIcon = lookup({ database: '🗄️', cache: '⚡', api: '🔌', queue: '📨' }, '📦')
export const linkIcon = lookup(
  { monitoring: '📊', errors: '🐛', docs: '📖', code: '💻', ci: '⚙️', web: '🌐', docker: '🐳' }, '🔗')

export function runtimeLabel(rt: string): string {
  return ({ 'docker-local': '🐳 Docker', 'argocd-k3s': '☸️ Kubernetes' } as Record<string, string>)[rt] || rt
}

export const shortURL = (u: string): string => u.replace(/^https?:\/\//, '')

// URL web du repo : la fiche la donne, sinon on la déduit de l'URL de clone
export const repoWebURL = (p: Project): string => p.service?.spec.repo || p.repo_url.replace(/\.git$/, '')

export const pipelinesURL = (p: Project): string | undefined =>
  p.service?.spec.links?.find(l => l.title === 'Pipelines')?.url

// Page d'édition de la fiche dans le repo (GitHub ou GitLab)
export function ficheURL(p: Project): string {
  const base = repoWebURL(p)
  return base.includes('github.com') ? `${base}/blob/main/catalog-info.yaml` : `${base}/-/blob/main/catalog-info.yaml`
}

export const deploymentsOf = (p: Project, all: Deployment[]): Deployment[] =>
  all.filter(d => (d.project || d.name) === p.name)

// Le déploiement du projet dans l'environnement env
export const deploymentIn = (p: Project, env: string, all: Deployment[]): Deployment | undefined =>
  deploymentsOf(p, all).find(d => d.environment === env)

export interface ProjectFilter { search: string; tier: string; lifecycle: string }

// Recherche dans le nom, la description, l'équipe, le langage et les tags
export function filterProjects(projects: Project[], f: ProjectFilter): Project[] {
  const q = f.search.trim().toLowerCase()
  return projects.filter(p => {
    const haystack = [p.name, p.description, owner(p), p.language, ...(p.service?.metadata.tags ?? [])]
    return (!q || haystack.some(v => v?.toLowerCase().includes(q)))
      && (!f.tier || tier(p) === f.tier)
      && (!f.lifecycle || lifecycle(p) === f.lifecycle)
  })
}

// healthDot : la santé Argo CD d'un objet en classe de pastille (.dot)
export function healthDot(health: string): string {
  switch (health) {
    case 'Healthy': return 'running'
    case 'Progressing': case 'Suspended': return 'deploying'
    case 'Degraded': case 'Missing': return 'error'
  }
  return ''
}
