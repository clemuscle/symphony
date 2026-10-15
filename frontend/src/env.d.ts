/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_VISIT?: string // '1' : mode visite (données de démo, lecture seule)
}
