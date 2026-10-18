import type { AdminStatus, ProviderCheck, ProviderKind, ProviderStatus } from '../types'

export const kindLabels: Record<ProviderKind, string> = {
  scm: 'Forge (SCM)',
  ci: 'CI',
  registry: "Registry d'images",
  runtime: 'Runtime',
  secrets: 'Secrets',
  identity: 'Identité',
}

// ago : « il y a 12 s », « il y a 3 min »… puis la date, en français quelle
// que soit la langue du navigateur
export function ago(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return 'jamais'
  const at = new Date(iso)
  if (at.getFullYear() < 2000) return 'jamais' // date zéro de Go
  const s = Math.round((now.getTime() - at.getTime()) / 1000)
  if (s < 5) return "à l'instant"
  if (s < 60) return `il y a ${s} s`
  if (s < 3600) return `il y a ${Math.floor(s / 60)} min`
  if (s < 86400) return `il y a ${Math.floor(s / 3600)} h`
  return 'le ' + at.toLocaleString('fr-FR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export type Level = 'ok' | 'error' | 'unknown'

// providerState : la vérification à la demande prime sur le dernier appel
// (plus récente, et elle sollicite vraiment les identifiants)
export function providerState(p: ProviderStatus, check?: ProviderCheck): { level: Level; text: string } {
  if (check) {
    return check.ok
      ? { level: 'ok', text: check.detail || 'répond' }
      : { level: 'error', text: check.error || 'en erreur' }
  }
  if (!p.last_call) return { level: 'unknown', text: 'pas encore appelé' }
  if (p.last_call.error) return { level: 'error', text: `${p.last_call.method} : ${p.last_call.error}` }
  return { level: 'ok', text: `${p.last_call.method} réussi` }
}

// problems : ce qui demande l'attention de l'admin, en une phrase chacun
export function problems(s: AdminStatus, checks: ProviderCheck[] = []): string[] {
  const out: string[] = []
  if (s.config.refused) out.push(`Configuration ${s.config.refused.revision} refusée : la précédente reste en place`)
  if (s.config.read_error) out.push('Configuration illisible dans sa source')
  if (s.config.secrets_error) out.push('Secrets de la configuration illisibles')
  const failed = new Set(checks.filter(c => !c.ok).map(c => `${c.kind}/${c.id}`))
  for (const p of s.providers) {
    const check = checks.find(c => c.kind === p.kind && c.id === p.id)
    if (providerState(p, check).level === 'error') failed.add(`${p.kind}/${p.id}`)
  }
  if (failed.size) out.push(`${failed.size} provider${failed.size > 1 ? 's' : ''} en erreur`)
  if (s.stacks.error) out.push('Repo des stacks illisible')
  const refused = s.stacks.refused.length
  if (refused) out.push(`${refused} stack${refused > 1 ? 's' : ''} refusée${refused > 1 ? 's' : ''}`)
  const fiches = s.catalogue.errors.length
  if (fiches) out.push(`${fiches} fiche${fiches > 1 ? 's' : ''} catalogue en erreur`)
  if (s.catalogue.error) out.push('Catalogue : liste des projets illisible')
  const legacy = s.catalogue.legacy?.length ?? 0
  if (legacy) out.push(`${legacy} fiche${legacy > 1 ? 's' : ''} à l'ancien format`)
  const unreadable = s.discovery.sources.filter(src => src.error).length
  if (unreadable) out.push(`Découverte : ${unreadable} groupe${unreadable > 1 ? 's' : ''} illisible${unreadable > 1 ? 's' : ''}`)
  const found = s.discovery.errors.length
  if (found) out.push(`Découverte : ${found} repo${found > 1 ? 's' : ''} en erreur`)
  const orphans = s.discovery.orphans.length
  if (orphans) out.push(`Découverte : ${orphans} projet${orphans > 1 ? 's' : ''} que sa source ne donne plus`)
  const warnings = (s.config.warnings || []).length + (s.stacks.warnings || []).length
  if (warnings) out.push(`${warnings} point${warnings > 1 ? 's' : ''} à corriger (ancien format…)`)
  return out
}

// stackName : la stack d'un fichier refusé (stacks/<nom>/stack.yaml, ou
// officiel:<nom> pour une stack du catalogue officiel)
export function stackName(file: string): string {
  if (file.startsWith('officiel:')) return file.slice('officiel:'.length)
  const parts = file.split('/')
  return parts.length >= 2 ? parts[parts.length - 2] : file
}
