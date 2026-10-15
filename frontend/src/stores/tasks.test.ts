import { describe, expect, it } from 'vitest'
import { activeTaskFor, taskStore, trackTask } from './tasks'
import type { Task } from '../types'

const task = (over: Partial<Task>): Task => ({
  id: 1, kind: 'deploy', resource: 'payment-api', status: 'RUNNING', steps: ['a', 'b'], step: 0,
  detail: '', result: null, created_by: 'bob', created_at: '2026-10-15T10:00:00Z',
  updated_at: '2026-10-15T10:00:00Z', ...over,
})

describe('tâches', () => {
  it('ne recule pas une tâche quand un événement ancien arrive après un récent', () => {
    trackTask(task({ step: 1, updated_at: '2026-10-15T10:00:05Z' }))
    trackTask(task({ step: 0, updated_at: '2026-10-15T10:00:01Z' }))
    expect(taskStore.byId[1].step).toBe(1)
  })

  it('trouve la tâche active d\'une ressource, par type', () => {
    trackTask(task({ id: 2, kind: 'delete_project', resource: 'auth-service' }))
    trackTask(task({ id: 3, kind: 'deploy', resource: 'auth-service', status: 'SUCCESS' }))
    expect(activeTaskFor('auth-service')?.id).toBe(2)
    expect(activeTaskFor('auth-service', 'deploy')).toBeUndefined()
  })
})
