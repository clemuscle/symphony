import { describe, expect, it } from 'vitest'
import type { Release } from '../types'
import { byProjectEnv, latestReleases, rollbackTarget, shortTag } from './releases'

const rel = (id: number, env: string, tag: string, status: Release['status'] = 'running', project = 'api'): Release => ({
  id, project_name: project, environment: env, deployment_id: `${project}-${env}`, runtime_provider: 'docker',
  image: 'reg/api', tag, port: 8080, status, url: '', origin: 'deploy', deployed_by: 'bob',
  created_at: '2026-10-19T10:00:00Z', updated_at: '2026-10-19T10:00:00Z',
})

describe('latestReleases', () => {
  it('garde la dernière version par projet et environnement', () => {
    const list = [rel(1, 'staging', 'a'), rel(3, 'staging', 'b'), rel(2, 'production', 'a'), rel(4, 'staging', 'x', 'running', 'web')]
    expect(latestReleases(list).map(r => r.id)).toEqual([4, 3, 2])
    expect(byProjectEnv(list)).toMatchObject({ api: { staging: { tag: 'b' }, production: { tag: 'a' } }, web: { staging: { tag: 'x' } } })
  })
})

describe('rollbackTarget', () => {
  const history = [rel(1, 'staging', 'v1'), rel(2, 'staging', 'v2', 'failed'), rel(3, 'staging', 'latest'), rel(4, 'staging', 'v3')]
  it('revient à la dernière version qui a démarré, au tag immuable, autre que celle en place', () => {
    expect(rollbackTarget(history)?.tag).toBe('v1')
    expect(rollbackTarget(history, 'reg/api:v1')?.tag).toBe('v3')
  })
  it("n'a rien à proposer sans version précédente", () => {
    expect(rollbackTarget([rel(1, 'staging', 'v1')])).toBeUndefined()
    expect(rollbackTarget([])).toBeUndefined()
  })
})

describe('shortTag', () => {
  it('raccourcit un sha', () => {
    expect(shortTag('0123456789abcdef0123456789abcdef01234567')).toBe('01234567')
    expect(shortTag('v1.2')).toBe('v1.2')
    expect(shortTag('')).toBe('latest')
  })
})
