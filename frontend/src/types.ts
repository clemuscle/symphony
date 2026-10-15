// Types des réponses de l'API de Symphony (clés JSON en snake_case, comme les
// tags json des structs Go)

export type Role = '' | 'viewer' | 'developer' | 'admin'

export interface User {
  subject: string
  name: string
  email: string
  groups: string[] | null
  teams: string[] // groupes qui ne donnent pas de rôle
  role: Role
}

export interface Me {
  auth_enabled: boolean
  user: User | null
}

export interface Link { title: string; url: string; icon: string }

export interface ActionInput {
  id: string
  label: string
  type: string // string, integer…
  required: boolean
  default?: string
  options?: string[]
  min?: number
  max?: number
}

export interface Action {
  name: string
  description: string
  webhook_url?: string
  ci_provider?: string
  pipeline?: string
  inputs: ActionInput[] | null
}

// Fiche catalogue d'un projet (catalog-info.yaml de son repo)
export interface Service {
  api_version: string
  kind: string
  metadata: {
    name: string
    owner: string
    team: { name: string; slack: string; email: string }
    tags: string[] | null
    tier: string
    lifecycle: string
    labels?: Record<string, string>
  }
  spec: {
    type: string
    language: string
    version: string
    repo: string
    registry: string
    docs: string
    links: Link[] | null
    actions: Action[] | null
    dependencies: { service: string; type: string }[] | null
    slo: { availability: string; latency_p99: string; error_budget: string }
    environments: { name: string; url: string; type: string; env?: Record<string, string> }[] | null
  }
}

export interface Environment {
  name: string
  runtime: string // runtime du projet dans cet environnement
  protected: boolean // promotion seulement
}

export interface Project {
  id: number
  name: string
  description: string
  language: string
  type: string
  port: number
  namespace: string
  scm_provider: string
  ci_provider: string
  registry_provider: string
  runtime_provider: string
  repo_path: string
  repo_url: string
  registry_url: string
  golden_path: string
  team: string
  created_by: string
  created_at: string
  deleting?: boolean
  service: Service | null // fiche catalogue, absente si le repo n'en a pas
  environments: Environment[] // vide : pas d'environnements configurés
}

export interface GoldenPath {
  id: string
  name: string
  description: string
  icon: string
  language: string
  type: string
  scm_provider: string
  ci_provider: string
  registry_provider: string
  runtime_provider: string
  repo: string
  branch: string
  skeleton: string
  ci_template: string
}

export interface Deployment {
  id: string
  name: string // nom du déploiement : <projet>-<environnement>
  project: string
  environment: string // vide : sans environnement
  image: string
  status: string // running, deploying, stopped, error
  url: string
  port: number
  created_at: string
  provider: string // runtime qui porte le déploiement
  sync?: string // Argo CD : Synced, OutOfSync
  health?: string // Argo CD : Healthy, Progressing, Degraded…
}

export interface Pipeline {
  id: string
  status: string
  web_url: string
  ref: string
  created_at: string
  finished_at: string | null
}

export type TaskStatus = 'PENDING' | 'RUNNING' | 'SUCCESS' | 'FAILED'

export interface Task {
  id: number
  kind: string
  resource: string
  status: TaskStatus
  steps: string[]
  step: number
  detail: string
  result: Record<string, unknown> | null
  error?: string
  wait?: { web_url?: string } | null // pipeline attendue
  created_by: string
  created_at: string
  updated_at: string
  finished_at?: string | null
}

export interface Group { name: string; path: string }

export interface IdentityGroups {
  provider?: string
  supported: boolean
  groups: Group[]
}

export interface ProjectImage {
  image: string
  port: number
  registry_provider: string
  runtime_provider: string
  tags: string[]
}

export interface CreateProjectRequest {
  name: string
  description: string
  namespace: string
  port: number
  owner: string
  tier: string
  lifecycle: string
  golden_path_id: string
  scm_provider?: string
  runtime_provider?: string
}

export interface DeployRequest {
  project_name: string
  environment?: string
  image: string
  tag: string
  port?: number
  host_port?: number
  runtime_provider?: string
}

export interface AuditEntry {
  id: number
  action: string
  resource: string
  details: Record<string, unknown> | null
  user_id: string
  ip_address: string
  created_at: string
}
