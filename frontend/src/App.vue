<template>
  <div class="layout" :class="{ visit: visitMode }">
    <div class="visit-banner" v-if="visitMode">
      👀 Mode visite : la vraie interface de Symphony sur des données capturées en démo, en
      lecture seule. Pour créer, déployer et promouvoir :
      <a href="https://github.com/clemuscle/symphony#essayer" target="_blank">./demo/up.sh</a>
    </div>
    <header class="header">
      <div class="brand">
        <span class="logo">🎼</span>
        <span class="name">Symphony</span>
        <span class="subtitle">Internal Developer Platform</span>
      </div>
      <nav class="nav">
        <RouterLink to="/" class="nav-link">🗂 Projets</RouterLink>
        <RouterLink to="/catalogue" class="nav-link">📦 Catalogue</RouterLink>
        <RouterLink to="/deployments" class="nav-link">🚀 Déploiements</RouterLink>
        <RouterLink to="/tasks" class="nav-link">
          ⚡ Activité<span class="count" v-if="activeTasks.length">{{ activeTasks.length }}</span>
        </RouterLink>
      </nav>
      <div class="user" v-if="session.loaded">
        <template v-if="session.authEnabled && session.user">
          <span class="user-name">{{ session.user.name || session.user.email }}</span>
          <span class="role-badge" :class="session.user.role || 'none'">{{ session.user.role || 'aucun rôle' }}</span>
          <!-- POST : une simple image ou un lien d'un autre site ne peut pas déconnecter -->
          <form method="post" action="/auth/logout" v-if="!visitMode">
            <button class="logout" type="submit">Déconnexion</button>
          </form>
        </template>
        <span class="role-badge none" v-else title="Pas de section auth dans la configuration">auth désactivée</span>
      </div>
    </header>
    <main class="main">
      <div class="no-role" v-if="session.loaded && !session.user?.role">
        Connecté·e, mais sans rôle Symphony : demander l'accès à l'équipe plateforme.
      </div>
      <RouterView v-else-if="session.loaded" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { visitMode } from './api'
import { session, loadSession } from './stores/session'
import { connectTasks, activeTasks } from './stores/tasks'

// Le flux des tâches s'ouvre une fois la session connue (et un rôle attribué)
onMounted(async () => {
  await loadSession()
  if (session.user?.role) connectTasks()
})
</script>

<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f0f2f5; color: #1a1a1a; }
.layout { display: flex; flex-direction: column; min-height: 100vh; }
.visit-banner { background: #fffbeb; color: #92400e; font-size: 13px; padding: 8px 24px; text-align: center; border-bottom: 1px solid #fde68a; }
.visit-banner a { color: #92400e; font-weight: 600; }
.header { background: #1a1a2e; color: white; padding: 0 24px; display: flex; align-items: center; justify-content: space-between; height: 56px; position: sticky; top: 0; z-index: 100; }
.brand { display: flex; align-items: center; gap: 10px; }
.logo { font-size: 22px; }
.name { font-weight: 700; font-size: 18px; }
.subtitle { color: #666; font-size: 13px; }
.nav { display: flex; gap: 4px; }
.nav-link { color: #aaa; text-decoration: none; padding: 6px 14px; border-radius: 6px; font-size: 14px; transition: all .15s; }
.nav-link:hover { color: white; background: #ffffff15; }
.nav-link.router-link-active { color: white; background: #667eea; }
.count { margin-left: 6px; background: #f6ad55; color: #1a1a2e; border-radius: 10px; padding: 0 7px; font-size: 12px; font-weight: 600; }
.user { display: flex; align-items: center; gap: 10px; font-size: 13px; }
.user-name { color: #ddd; }
.role-badge { padding: 2px 8px; border-radius: 10px; font-size: 12px; background: #ffffff20; color: #ddd; }
.role-badge.admin { background: #e53e3e; color: white; }
.role-badge.developer { background: #667eea; color: white; }
.role-badge.viewer { background: #38a169; color: white; }
.logout { background: transparent; border: 1px solid #ffffff40; color: #ddd; padding: 4px 10px; border-radius: 6px; cursor: pointer; font-size: 12px; }
.logout:hover { background: #ffffff15; }
.no-role { background: #fffbea; border: 1px solid #f6e05e; padding: 16px; border-radius: 8px; }
.main { flex: 1; padding: 28px; max-width: 1200px; margin: 0 auto; width: 100%; }
</style>
