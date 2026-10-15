import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DayTwoAction from './DayTwoAction.vue'
import { api } from '../api'
import type { Action, Task } from '../types'

const action: Action = {
  name: 'Rollback', description: 'Revenir à une version', inputs: [
    { id: 'version', label: 'Version', type: 'string', required: true },
    { id: 'reason', label: 'Raison', type: 'string', required: false },
  ],
}

describe('action Day-2', () => {
  it('n\'envoie que les entrées renseignées, puis suit la tâche', async () => {
    const t = { id: 42, kind: 'action_pipeline', resource: 'payment-api', status: 'PENDING', steps: ['Pipeline'],
      step: 0, detail: '', result: null, created_by: 'bob', created_at: '', updated_at: '' } as Task
    const spy = vi.spyOn(api, 'triggerAction').mockResolvedValue({ data: t } as never)
    const w = mount(DayTwoAction, { props: { action, project: 'payment-api', allowed: true } })
    await w.findAll('input')[0].setValue('v1.2')
    await w.find('button').trigger('click')
    await flushPromises()
    expect(spy).toHaveBeenCalledWith('payment-api', 'Rollback', { version: 'v1.2' })
    expect(w.text()).toContain('#42')
  })

  it('n\'affiche pas le bouton sans droit sur le projet', () => {
    const w = mount(DayTwoAction, { props: { action, project: 'payment-api', allowed: false } })
    expect(w.find('button').exists()).toBe(false)
  })
})
