import { describe, expect, it } from 'vitest'
import { ago, problems, providerState, templateName } from './admin'
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
    config: { source: 's', revision: 'a', applied_at: '', checked_at: '' },
    providers: [gitlab],
    golden_paths: { repo: 'r', branch: 'main', path: 'templates', loaded_at: '', loaded: ['api'], refused: [] },
    catalogue: { file: 'catalog-info.yaml', synced_at: '', errors: [], missing: [] },
  }
}

describe('problems', () => {
  it('rien à signaler', () => {
    expect(problems(status())).toEqual([])
  })
  it('liste ce qui demande attention', () => {
    const s = status()
    s.config.refused = { revision: 'b12', at: '', error: 'clé inconnue' }
    s.golden_paths.refused = [{ file: 'templates/x/template.yaml', error: 'e' }, { file: 'templates/y/template.yaml', error: 'e' }]
    s.catalogue.errors = [{ project: 'p', error: 'e' }]
    const checks = [{ kind: 'scm' as const, id: 'gitlab', ok: false, error: 'jeton refusé', duration_ms: 3 }]
    expect(problems(s, checks)).toEqual([
      'Configuration b12 refusée : la précédente reste en place',
      '1 provider en erreur',
      '2 golden paths refusés',
      '1 fiche catalogue en erreur',
    ])
  })
})

describe('templateName', () => {
  it('le dossier du template', () => {
    expect(templateName('templates/api-rest-go/template.yaml')).toBe('api-rest-go')
    expect(templateName('template.yaml')).toBe('template.yaml')
  })
})
