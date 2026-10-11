import { reactive } from 'vue'
import { api } from '../api'

// Utilisateur connecté et son rôle (viewer < developer < admin), lus sur
// /api/v1/me. Le serveur reste seul juge : l'UI masque seulement ce que le
// rôle ne permet pas.
const rank = { viewer: 1, developer: 2, admin: 3 }

export const session = reactive({
  loaded: false,
  authEnabled: false,
  user: null,
})

export async function loadSession() {
  const { data } = await api.me() // 401 : l'intercepteur d'api.js redirige vers la connexion
  session.authEnabled = data.auth_enabled
  session.user = data.user
  session.loaded = true
}

// can('developer') : le rôle de l'utilisateur est au moins developer
export function can(role) {
  return (rank[session.user?.role] || 0) >= rank[role]
}
