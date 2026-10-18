// Icônes de l'interface : logos des outils (Simple Icons, CC0) et pictogrammes
// (Lucide, ISC), par nom. Seules les icônes importées ici entrent dans le
// build. Rendues par components/AppIcon.vue.
import type { Component } from 'vue'
import {
  siArgo, siBackstage, siDocker, siGithub, siGitlab, siGo, siGrafana, siHelm, siKeycloak, siKubernetes,
  siNodedotjs, siOpenjdk, siOpenid, siPostgresql, siPrometheus, siPython, siSentry, siVault,
} from 'simple-icons'
import {
  Activity, ArrowUp, ArrowUpRight, Ban, BookOpen, Boxes, Bug, ChartLine, Check, CircleCheck, CircleX, Code,
  Construction, Database, Eye, FlaskConical, FolderKanban, Globe, Hourglass, KeyRound, Link, Lock, Mail,
  MessageSquare, Minus, Music, Package, Pencil, Play, Plug, Puzzle, RefreshCw, Rocket, RotateCw, ScanSearch, ScrollText,
  Search, Settings, Settings2, Square, Trash2, TriangleAlert, User, Workflow, X, Zap,
} from '@lucide/vue'

export interface Brand { title: string; path: string; hex: string }

// Logos, par langage, plateforme ou outil
const brands: Record<string, Brand> = {
  go: siGo, python: siPython, node: siNodedotjs, nodejs: siNodedotjs, java: siOpenjdk,
  gitlab: siGitlab, github: siGithub, docker: siDocker, kubernetes: siKubernetes, argocd: siArgo,
  vault: siVault, keycloak: siKeycloak, oidc: siOpenid, postgres: siPostgresql, postgresql: siPostgresql,
  helm: siHelm, backstage: siBackstage, prometheus: siPrometheus, grafana: siGrafana, sentry: siSentry,
}

// Couleurs de marque illisibles sur fond blanc
const readable: Record<string, string> = { vault: '000000', backstage: '36BAA2' }

// Pictogrammes : liens et dépendances des fiches, cycle de vie, interface
const pictos: Record<string, Component> = {
  // dépendances (types de l'ancien format, kinds Backstage)
  database: Database, cache: Zap, api: Plug, queue: Mail, resource: Database, component: Puzzle,
  // liens des fiches (icon)
  monitoring: ChartLine, errors: Bug, docs: BookOpen, code: Code, ci: Workflow, web: Globe, link: Link,
  // cycle de vie
  experimental: FlaskConical, staging: Construction, production: CircleCheck, deprecated: Ban,
  // interface
  projects: FolderKanban, catalogue: Package, deployments: Rocket, activity: Activity, admin: Settings,
  logo: Music, visit: Eye, search: Search, refresh: RefreshCw, close: X, pending: Hourglass, error: CircleX, warning: TriangleAlert,
  ok: Check, user: User, chat: MessageSquare, mail: Mail, edit: Pencil, delete: Trash2, remove: Minus,
  promote: ArrowUp, external: ArrowUpRight, restart: RotateCw, config: Settings2, providers: Plug,
  stacks: Boxes, fiche: ScrollText, audit: ScrollText, discovered: ScanSearch, lock: Lock, action: Zap,
  run: Play, stop: Square, secret: KeyRound, package: Package,
}

export type IconSource = { brand: Brand } | { picto: Component }

// icon : le logo ou le pictogramme d'un nom (minuscules), sinon fallback
export function icon(name: string | undefined, fallback = 'package'): IconSource {
  const key = (name || '').toLowerCase()
  const b = brands[key]
  if (b) return { brand: { ...b, hex: readable[key] || b.hex } }
  return { picto: pictos[key] || pictos[fallback] || Package }
}

export const hasIcon = (name: string): boolean => name.toLowerCase() in brands || name.toLowerCase() in pictos

// runtimeKind : la technologie d'un runtime, d'après son id (la page Projets
// ne voit que les ids) : kubernetes pour Argo CD, docker
export function runtimeKind(id: string | undefined): 'kubernetes' | 'docker' | '' {
  const s = (id || '').toLowerCase()
  if (/argo|k8s|k3s|kube/.test(s)) return 'kubernetes'
  if (s.includes('docker')) return 'docker'
  return ''
}

export function runtimeLabel(id: string | undefined): string {
  return ({ kubernetes: 'Kubernetes', docker: 'Docker', '': id || '' } as const)[runtimeKind(id)]
}
