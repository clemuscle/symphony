import { describe, expect, it } from 'vitest'
import { formatValue, keepFile, paramErrors, visible, fillDefaults } from './parameters'
import type { StackParameter } from '../types'

const params: StackParameter[] = [
  { name: 'version', title: 'Version', type: 'choice', choices: ['1.22', '1.23'], default: '1.22', locked: true },
  { name: 'metrics', title: 'Métriques', type: 'boolean', default: true },
  { name: 'metrics_path', title: 'Chemin', type: 'string', default: '/metrics', required: true, when: { metrics: true } },
  { name: 'database', title: 'Base', type: 'choice', choices: ['aucune', 'postgres', 'mysql'], default: 'aucune' },
  { name: 'db_name', title: 'Nom', type: 'string', required: true, when: { database: ['postgres', 'mysql'] } },
  { name: 'replicas', title: 'Réplicas', type: 'number' },
]

describe('visible', () => {
  it('suit la condition when', () => {
    expect(visible(params[2], { metrics: true })).toBe(true)
    expect(visible(params[2], { metrics: false })).toBe(false)
    expect(visible(params[4], { database: 'mysql' })).toBe(true)
    expect(visible(params[4], {})).toBe(false)
    expect(visible(params[0], {})).toBe(true)
  })
})

describe('fillDefaults', () => {
  it('complète les défauts des paramètres qui existent', () => {
    expect(fillDefaults(params, {})).toEqual({ version: '1.22', metrics: true, metrics_path: '/metrics', database: 'aucune' })
  })
  it('garde les saisies, retire les paramètres sans objet, impose les valeurs verrouillées', () => {
    const v = fillDefaults(params, { version: '1.23', metrics: false, metrics_path: '/m', database: 'postgres', db_name: 'x', replicas: 2 })
    expect(v).toEqual({ version: '1.22', metrics: false, database: 'postgres', db_name: 'x', replicas: 2 })
  })
})

describe('paramErrors', () => {
  it('requis et nombres', () => {
    expect(paramErrors(params, fillDefaults(params, { database: 'postgres' }))).toEqual({ db_name: 'Requis' })
    expect(paramErrors(params, { metrics: true, metrics_path: '' })).toEqual({ metrics_path: 'Requis' })
    expect(paramErrors(params, { replicas: Number.NaN })).toEqual({ replicas: 'Nombre attendu' })
    expect(paramErrors(params, fillDefaults(params, {}))).toEqual({})
  })
})

describe('formatValue', () => {
  it('booléens en français', () => {
    expect(formatValue(true)).toBe('oui')
    expect(formatValue(false)).toBe('non')
    expect(formatValue(3)).toBe('3')
    expect(formatValue(undefined)).toBe('')
  })
})

describe('keepFile', () => {
  it('suit spec.files', () => {
    const st = { files: [{ path: 'deploy/infra/', when: { postgres: true } }] }
    expect(keepFile(st, 'deploy/infra', { postgres: true })).toBe(true)
    expect(keepFile(st, 'deploy/infra', { postgres: false })).toBe(false)
    expect(keepFile(st, 'deploy/infra/db.yaml', {})).toBe(false)
    expect(keepFile(st, 'main.py', {})).toBe(true)
    expect(keepFile({}, 'deploy/infra', {})).toBe(true)
  })
})
