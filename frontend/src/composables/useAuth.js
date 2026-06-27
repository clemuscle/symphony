import { reactive } from 'vue'
import { api } from '../api'

const state = reactive({ user: null, loading: true })

async function init() {
  try {
    const r = await api.getMe()
    state.user = r.data
  } catch {
    state.user = null
  } finally {
    state.loading = false
  }
}

function login() {
  const base = import.meta.env.VITE_API_URL || 'http://localhost:8090'
  window.location.href = `${base}/auth/login`
}

function logout() {
  const base = import.meta.env.VITE_API_URL || 'http://localhost:8090'
  window.location.href = `${base}/auth/logout`
}

export function useAuth() {
  return { state, init, login, logout }
}
