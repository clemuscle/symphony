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
  stack: string
  stack_parameters: Record<string, ParamValue> // valeurs des paramètres de la stack, à la création
  team: string
  created_by: string
  created_at: string
  deleting?: boolean
  service: Service | null // fiche catalogue, absente si le repo n'en a pas
  environments: Environment[] // vide : pas d'environnements configurés
}

// Stack : modèle de projet, du repo de l'entreprise ou du catalogue officiel
export interface Stack {
  id: string
  name: string
  description: string
  icon: string
  language: string
  type: string
  // Providers résolus (ceux par défaut si la stack n'en désigne pas)
  scm_provider: string
  ci_provider: string
  registry_provider: string
  runtime_provider: string
  official: boolean
  repo?: string
  branch?: string
  ci_template: string
  environments?: Record<string, string>
  parameters: StackParameter[]
  schema: Record<string, unknown> // JSON Schema des paramètres, appliqué par l'API
  kubernetes?: { helm?: string; kustomize?: string }
}

export type ParamValue = string | number | boolean

// Un champ du formulaire d'une stack
export interface StackParameter {
  name: string
  title: string
  description?: string
  type: 'string' | 'number' | 'boolean' | 'choice'
  choices?: string[]
  default?: ParamValue
  required?: boolean
  // Le paramètre n'existe que si ces paramètres ont ces valeurs (ou l'une
  // d'une liste)
  when?: Record<string, ParamValue | ParamValue[]>
  locked?: boolean
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
  stack: string
  parameters: Record<string, ParamValue>
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

// Page Administration (GET /api/v1/admin/status)
export type ProviderKind = 'scm' | 'ci' | 'registry' | 'runtime' | 'secrets' | 'identity'

export interface ConfigStatus {
  source: string
  revision: string
  applied_at: string
  checked_at: string
  read_error?: string
  secrets_error?: string
  refused?: { revision: string; at: string; error: string }
  warnings: string[] // configuration appliquée, mais à corriger
}

export interface ProviderStatus {
  kind: ProviderKind
  id: string
  type: string
  default: boolean
  last_call: { method: string; at: string; error?: string } | null
}

export interface AdminStatus {
  config: ConfigStatus
  providers: ProviderStatus[]
  stacks: {
    repo: string // vide : pas de repo de stacks
    branch: string
    path: string
    official: string[] // stacks officielles exposées ('*' : toutes)
    loaded_at: string
    error?: string
    loaded: string[]
    refused: { file: string; error: string }[]
    warnings: string[]
  }
  catalogue: {
    file: string
    synced_at: string
    error?: string
    errors: { project: string; error: string }[]
    missing: string[]
  }
}

// Vérification d'un provider (GET /api/v1/admin/providers/check)
export interface ProviderCheck {
  kind: ProviderKind
  id: string
  ok: boolean
  detail?: string
  error?: string
  duration_ms: number
}
