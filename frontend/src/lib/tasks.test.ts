import { describe, expect, it } from 'vitest'
import { taskTitle } from './tasks'

describe('taskTitle', () => {
  it('donne le type de tâche', () => {
    expect(taskTitle({ kind: 'create_project', result: null })).toBe('Création')
    expect(taskTitle({ kind: 'inconnu', result: {} })).toBe('inconnu')
  })
  it("dit l'environnement d'un déploiement", () => {
    expect(taskTitle({ kind: 'deploy', result: { target: { environment: 'staging' } } })).toBe('Déploiement en staging')
  })
  it("dit d'où part une promotion", () => {
    expect(taskTitle({ kind: 'deploy', result: { target: { environment: 'production', promoted_from: 'staging' } } }))
      .toBe('Promotion staging → production')
  })
  it('se contente du type sans cible (tâche en file, ancienne tâche)', () => {
    expect(taskTitle({ kind: 'deploy', result: {} })).toBe('Déploiement')
  })
})
