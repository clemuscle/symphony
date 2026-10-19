// Historique des déploiements : matrice versions × environnements et retour
// arrière (fonctions pures, testées dans releases.test.ts)
import type { Release, ReleaseOrigin } from '../types'

export const releaseRef = (r: Pick<Release, 'image' | 'tag'>): string => `${r.image}:${r.tag || 'latest'}`

// Un tag de commit (sha) se lit sur ses 8 premiers caractères
export const shortTag = (tag: string): string => (/^[0-9a-f]{40}$/.test(tag) ? tag.slice(0, 8) : tag || 'latest')

export const originLabels: Record<ReleaseOrigin, string> = {
  deploy: 'déploiement', promotion: 'promotion', rollback: 'retour arrière',
}

// ranOnce : la version a démarré dans son environnement (même arrêtée depuis)
export const ranOnce = (r: Pick<Release, 'status'>): boolean => r.status === 'running' || r.status === 'stopped'

// latestReleases : la dernière version de chaque projet dans chaque
// environnement, comme GET /releases?latest=true (le mode visite la calcule)
export function latestReleases(list: Release[]): Release[] {
  const latest = new Map<string, Release>()
  for (const r of list) {
    const key = r.project_name + '\u0000' + r.environment
    const prev = latest.get(key)
    if (!prev || r.id > prev.id) latest.set(key, r)
  }
  return [...latest.values()].sort((a, b) => b.id - a.id)
}

// byProjectEnv : versions indexées par projet puis environnement (matrice)
export function byProjectEnv(list: Release[]): Record<string, Record<string, Release>> {
  const out: Record<string, Record<string, Release>> = {}
  for (const r of latestReleases(list)) (out[r.project_name] ??= {})[r.environment] = r
  return out
}

// rollbackTarget : la version à laquelle un retour arrière revient, comme le
// serveur la choisit : la plus récente qui a démarré, au tag immuable, et dont
// l'image diffère de celle en place (current ; vide : la dernière de l'historique)
export function rollbackTarget(history: Release[], current = ''): Release | undefined {
  const sorted = [...history].sort((a, b) => b.id - a.id)
  const now = current || (sorted[0] ? releaseRef(sorted[0]) : '')
  return sorted.find(r => ranOnce(r) && r.tag && r.tag !== 'latest' && releaseRef(r) !== now)
}
