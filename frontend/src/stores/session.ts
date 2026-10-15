import { reactive } from 'vue'
import { api } from '../api'
import type { Role, User } from '../types'

// Utilisateur connecté et son rôle (viewer < developer < admin), lus sur
// /api/v1/me. Le serveur reste seul juge : l'UI masque seulement ce que le
// rôle ne permet pas.
const rank: Record<Role, number> = { '': 0, viewer: 1, developer: 2, admin: 3 }

export const session = reactive<{ loaded: boolean; authEnabled: boolean; user: User | null }>({
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
export function can(role: Exclude<Role, ''>): boolean {
  return rank[session.user?.role ?? ''] >= rank[role]
}

// Équipes de l'utilisateur : ses groupes qui ne donnent pas de rôle
export const myTeams = (): string[] => session.user?.teams ?? []

// canOperate(team) : peut déployer et lancer les actions d'un projet de cette
// équipe (admin, ou developer membre de l'équipe) ; même règle que le serveur
export function canOperate(team: string | undefined): boolean {
  return can('admin') || (can('developer') && !!team && myTeams().includes(team))
}
