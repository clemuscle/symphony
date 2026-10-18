import { describe, expect, it } from 'vitest'
import { ago, problems, providerState, stackName } from './admin'
import type { AdminStatus, ProviderStatus } from '../types'

const now = new Date('2026-10-17T10:00:00Z')

describe('ago', () => {
  it('dit le temps écoulé', () => {
    expect(ago('2026-10-17T09:59:58Z', now)).toBe("à l'instant")
    expect(ago('2026-10-17T09:59:48Z', now)).toBe('il y a 12 s')
    expect(ago('2026-10-17T09:57:00Z', now)).toBe('il y a 3 min')
    expect(ago('2026-10-17T08:00:00Z', now)).toBe('il y a 2 h')
    expect(ago('2026-10-15T08:00:00Z', now)).toMatch(/^le 15\/10/)
  })
  it('reconnaît la date zéro de Go et l’absence de date', () => {
    expect(ago('0001-01-01T00:00:00Z', now)).toBe('jamais')
    expect(ago(undefined, now)).toBe('jamais')
  })
})

const gitlab: ProviderStatus = { kind: 'scm', id: 'gitlab', type: 'gitlab', default: true, last_call: null }

describe('providerState', () => {
  it('sans appel ni vérification : inconnu', () => {
    expect(providerState(gitlab).level).toBe('unknown')
  })
  it('le dernier appel en erreur', () => {
    const p = { ...gitlab, last_call: { method: 'ListFiles', at: '', error: 'status 401' } }
    expect(providerState(p)).toEqual({ level: 'error', text: 'ListFiles : status 401' })
  })
  it('la vérification prime sur le dernier appel', () => {
    const p = { ...gitlab, last_call: { method: 'ListFiles', at: '', error: 'status 401' } }
    const check = { kind: 'scm' as const, id: 'gitlab', ok: true, detail: 'connecté en tant que root', duration_ms: 12 }
    expect(providerState(p, check)).toEqual({ level: 'ok', text: 'connecté en tant que root' })
  })
})

function status(): AdminStatus {
  return {
    config: { source: 's', revision: 'a', applied_at: '', checked_at: '', warnings: [] },
    providers: [gitlab],
    stacks: { repo: 'r', branch: 'main', path: 'stacks', official: ['*'], loaded_at: '', loaded: ['api'], refused: [], warnings: [] },
    catalogue: { file: 'catalog-info.yaml', synced_at: '', errors: [], missing: [], legacy: [] },
    discovery: { enabled: false, interval: '5m', scan_at: null, sources: [], errors: [], orphans: [] },
  }
}

describe('problems', () => {
  it('rien à signaler', () => {
    expect(problems(status())).toEqual([])
  })
  it('liste ce qui demande attention', () => {
    const s = status()
    s.config.refused = { revision: 'b12', at: '', error: 'clé inconnue' }
    s.stacks.refused = [{ file: 'stacks/x/stack.yaml', error: 'e' }, { file: 'officiel:y', error: 'e' }]
    s.stacks.warnings = ['stacks/z/template.yaml : ancien format']
    s.catalogue.errors = [{ project: 'p', error: 'e' }]
    s.catalogue.legacy = ['auth', 'billing']
    s.discovery.sources = [{ key: 'gitlab/x', scm: 'gitlab', group: 'x', ci: 'gitlab-ci', registry: '', repos: 0,
      projects: [], imported: [], excluded: 0, no_fiche: 0, error: 'groupe gitlab x : introuvable' }]
    s.discovery.errors = [{ source: 'gitlab/y', repo: 'y/z', error: 'nom déjà pris' }]
    s.discovery.orphans = [{ project: 'old', source: 'gitlab/y', repo: 'y/old' }]
    const checks = [{ kind: 'scm' as const, id: 'gitlab', ok: false, error: 'jeton refusé', duration_ms: 3 }]
    expect(problems(s, checks)).toEqual([
      'Configuration b12 refusée : la précédente reste en place',
      '1 provider en erreur',
      '2 stacks refusées',
      '1 fiche catalogue en erreur',
      "2 fiches à l'ancien format",
      'Découverte : 1 groupe illisible',
      'Découverte : 1 repo en erreur',
      'Découverte : 1 projet que sa source ne donne plus',
      '1 point à corriger (ancien format…)',
    ])
  })
})

describe('stackName', () => {
  it('le dossier de la stack, ou la stack officielle', () => {
    expect(stackName('stacks/api-rest-go/stack.yaml')).toBe('api-rest-go')
    expect(stackName('officiel:worker-python')).toBe('worker-python')
    expect(stackName('stack.yaml')).toBe('stack.yaml')
  })
})
