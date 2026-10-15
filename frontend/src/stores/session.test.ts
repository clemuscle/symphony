import { beforeEach, describe, expect, it } from 'vitest'
import { can, canOperate, session } from './session'
import type { User } from '../types'

const user = (role: User['role'], teams: string[]): User =>
  ({ subject: 's', name: 'n', email: '', groups: [], teams, role })

describe('droits dans l\'UI (mêmes règles que le serveur)', () => {
  beforeEach(() => { session.user = null })

  it('compare les rôles', () => {
    session.user = user('developer', [])
    expect(can('viewer')).toBe(true)
    expect(can('developer')).toBe(true)
    expect(can('admin')).toBe(false)
    session.user = null
    expect(can('viewer')).toBe(false)
  })

  it('réserve un projet aux developers de son équipe et aux admins', () => {
    session.user = user('developer', ['team-payments'])
    expect(canOperate('team-payments')).toBe(true)
    expect(canOperate('team-identity')).toBe(false)
    expect(canOperate('')).toBe(false) // projet sans équipe : admins seulement
    session.user = user('viewer', ['team-payments'])
    expect(canOperate('team-payments')).toBe(false)
    session.user = user('admin', [])
    expect(canOperate('team-identity')).toBe(true)
  })
})
