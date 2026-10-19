import { describe, expect, it } from 'vitest'
import { awaitingApproval, taskTitle } from './tasks'

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

describe('retour arrière et approbation', () => {
  it('dit un retour arrière', () => {
    expect(taskTitle({ kind: 'deploy', result: { target: { environment: 'production', origin: 'rollback' } } }))
      .toBe('Retour arrière en production')
  })
  it("reconnaît une tâche qui attend un lead", () => {
    expect(awaitingApproval({ status: 'RUNNING', wait: { approval: true } })).toBe(true)
    expect(awaitingApproval({ status: 'RUNNING', wait: { web_url: 'x' } })).toBe(false)
    expect(awaitingApproval({ status: 'FAILED', wait: { approval: true } })).toBe(false)
  })
})
