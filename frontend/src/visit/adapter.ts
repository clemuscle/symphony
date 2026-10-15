// Mode visite : l'interface, sans serveur. Les appels à l'API sont servis
// depuis des données capturées sur la démo (fixtures.json) ; toute écriture est
// refusée. Compilé seulement avec VITE_VISIT=1 (npm run build:visit), pour la
// version publiée sur GitHub Pages.
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import fixtures from './fixtures.json'

export const readOnlyMessage =
  "Mode visite : lecture seule, sur des données de démo. Pour créer, déployer ou promouvoir, lancer la démo complète (./demo/up.sh)."

type Fixtures = Record<string, unknown>
const data = fixtures as Fixtures

// route : chemin de l'API sans paramètres de requête, ni /api/v1
function route(url: string): string {
  return url.replace(/^.*\/api\/v1/, '').split('?')[0]
}

// Réponse pour un GET : la fixture du chemin exact, sinon une règle par motif
export function visitResponse(url: string, params?: Record<string, unknown>): { status: number; body: unknown } {
  const path = route(url)
  if (path in data) {
    const body = data[path]
    // ?resource= : les tâches d'un projet
    if (path === '/tasks' && params?.resource && Array.isArray(body)) {
      return { status: 200, body: body.filter((t: { resource?: string }) => t.resource === params.resource) }
    }
    return { status: 200, body }
  }
  const image = path.match(/^\/projects\/([^/]+)\/image$/)
  if (image) {
    const images = (data['images'] ?? {}) as Record<string, unknown>
    return images[image[1]] ? { status: 200, body: images[image[1]] } : { status: 404, body: { error: 'projet introuvable' } }
  }
  if (/^\/deployments\/[^/]+\/logs$/.test(path)) {
    return { status: 200, body: { logs: '(mode visite : pas de journaux)\n' } }
  }
  return { status: 404, body: { error: 'introuvable en mode visite : ' + path } }
}

export const visitAdapter: AxiosAdapter = async (config: InternalAxiosRequestConfig) => {
  const method = (config.method ?? 'get').toUpperCase()
  const url = config.url ?? ''
  const { status, body } = method === 'GET'
    ? visitResponse(url, config.params)
    : { status: 403, body: { error: readOnlyMessage } }
  const response = { data: body, status, statusText: String(status), headers: new AxiosHeaders(), config }
  if (status >= 400) {
    throw new AxiosError(String((body as { error?: string }).error), String(status), config, null, response)
  }
  return response
}
