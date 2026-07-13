import { reactive, computed } from 'vue'
import { api } from '../api'

const state = reactive({ user: null, loading: true })

// true if the user can create projects, trigger pipelines, deploy staging (recettes)
const canDevelop = computed(() =>
  state.user?.dev_mode || ['admin', 'lead', 'developer'].includes(state.user?.role)
)

// true if the user can deploy to production and stop production containers
const canDeploy = computed(() =>
  state.user?.dev_mode || ['admin', 'lead'].includes(state.user?.role)
)

// true if the user can access admin-only actions (setup, config reload, template reload)
const isAdmin = computed(() =>
  state.user?.dev_mode || state.user?.role === 'admin'
)

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
  return { state, canDevelop, canDeploy, isAdmin, init, login, logout }
}
