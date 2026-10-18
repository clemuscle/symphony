import { describe, expect, it } from 'vitest'
import { ficheURL, filterProjects, lifecycleLabel, owner, repoWebURL, tier } from './projects'
import type { Project, Service } from '../types'

function project(over: Partial<Project> = {}, svc: Partial<Service['metadata']> | null = {}): Project {
  return {
    id: 1, name: 'payment-api', description: 'Paiements', language: 'go', type: 'api', port: 8080,
    namespace: '', scm_provider: 'gitlab', ci_provider: 'gitlab-ci', registry_provider: '',
    runtime_provider: 'docker-local', repo_path: 'apps/payment-api',
    repo_url: 'http://gitlab/apps/payment-api.git', registry_url: '', stack: 'api-rest-go', stack_parameters: {},
    team: 'team-payments', created_by: 'bob', created_at: '', branch: 'main', discovered_from: '', environments: [],
    service: svc === null ? null : {
      api_version: 'symphony/v1', kind: 'Service',
      metadata: {
        name: 'payment-api', owner: 'team-payments', team: { name: '', slack: '', email: '' },
        tags: ['paiement'], tier: 'critical', lifecycle: 'production', ...svc,
      },
      spec: {
        type: 'api', language: 'go', version: '', repo: '', registry: '', docs: '', links: null,
        actions: null, dependencies: null, slo: { availability: '', latency_p99: '', error_budget: '' },
        environments: null,
      },
    },
    ...over,
  }
}

describe('projets', () => {
  it('prend le tier et l\'owner de la fiche, avec des défauts sans fiche', () => {
    expect(tier(project())).toBe('critical')
    expect(tier(project({}, null))).toBe('standard')
    expect(owner(project({}, null))).toBe('team-payments') // l'équipe du projet
  })

  it('déduit l\'URL du repo et de la fiche (GitLab ou GitHub)', () => {
    expect(repoWebURL(project({}, null))).toBe('http://gitlab/apps/payment-api')
    expect(ficheURL(project({}, null))).toBe('http://gitlab/apps/payment-api/-/blob/main/catalog-info.yaml')
    // Projet découvert : la branche par défaut de son repo
    expect(ficheURL(project({ branch: 'master' }, null))).toBe('http://gitlab/apps/payment-api/-/blob/master/catalog-info.yaml')
    const gh = project({ repo_url: 'https://github.com/me/api.git' }, null)
    expect(ficheURL(gh)).toBe('https://github.com/me/api/blob/main/catalog-info.yaml')
  })

  it('filtre par texte (nom, équipe, tags), tier et cycle de vie', () => {
    const list = [project(), project({ id: 2, name: 'auth-service', description: 'Authentification', team: 'team-identity' }, { owner: 'team-identity', tier: 'standard', tags: [] })]
    const names = (f: Parameters<typeof filterProjects>[1]) => filterProjects(list, f).map(p => p.name)
    expect(names({ search: 'PAIEMENT', tier: '', lifecycle: '' })).toEqual(['payment-api'])
    expect(names({ search: 'identity', tier: '', lifecycle: '' })).toEqual(['auth-service'])
    expect(names({ search: '', tier: 'critical', lifecycle: '' })).toEqual(['payment-api'])
    expect(names({ search: '  ', tier: '', lifecycle: 'production' })).toHaveLength(2)
  })
})

describe('environnements', () => {
  it('rattache les déploiements au projet et à leur environnement', async () => {
    const { deploymentIn, deploymentsOf } = await import('./projects')
    const p = project()
    const d = (name: string, proj: string, env: string) =>
      ({ id: name, name, project: proj, environment: env, image: '', status: 'running', url: '', port: 0, created_at: '', provider: 'docker-local' })
    const all = [d('payment-api-staging', 'payment-api', 'staging'), d('payment-api-production', 'payment-api', 'production'),
      d('payment-api-v2-staging', 'payment-api-v2', 'staging')]
    expect(deploymentsOf(p, all)).toHaveLength(2)
    expect(deploymentIn(p, 'production', all)?.name).toBe('payment-api-production')
    expect(deploymentIn(p, 'dev', all)).toBeUndefined()
  })
})

describe('lifecycleLabel', () => {
  it("ne reprend pas les noms d'environnement", () => {
    expect(lifecycleLabel('production')).toBe('en service')
    expect(lifecycleLabel('staging')).toBe('en rodage')
    expect(lifecycleLabel('autre')).toBe('autre')
  })
})
