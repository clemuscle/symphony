import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'vitest'
import { readOnlyMessage, visitAdapter, visitResponse } from './adapter'

describe('mode visite', () => {
  it('sert les données capturées, en lecture', () => {
    expect(visitResponse('/api/v1/me').status).toBe(200)
    expect((visitResponse('/api/v1/me').body as { user: { role: string } }).user.role).toBe('viewer')
    expect(visitResponse('/api/v1/projects?x=1').status).toBe(200)
    expect(visitResponse('/api/v1/inexistant').status).toBe(404)
    expect(visitResponse('/api/v1/deployments/a/logs').status).toBe(200)
  })

  it('refuse toute écriture avec un message qui renvoie à la démo complète', async () => {
    const config = { method: 'post', url: '/api/v1/deployments', headers: new AxiosHeaders() }
    await expect(visitAdapter(config)).rejects.toSatisfy((e: AxiosError) =>
      e.response?.status === 403 && (e.response.data as { error: string }).error === readOnlyMessage)
  })
})
