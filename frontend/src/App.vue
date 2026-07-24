<template>
  <div class="layout">
    <header v-if="auth.state.user && route.path !== '/setup'" class="header" :class="{ 'admin-mode': inAdminMode }">
      <div class="brand">
        <span class="logo">🎼</span>
        <span class="name">Symphony</span>
        <span class="subtitle">{{ inAdminMode ? 'Mode admin' : 'Internal Developer Portal' }}</span>
      </div>
      <nav class="nav">
        <RouterLink to="/" class="nav-link">🗂 Projets</RouterLink>
        <RouterLink to="/catalogue" class="nav-link">📦 Catalogue</RouterLink>
        <RouterLink to="/deployments" class="nav-link">🚀 Déploiements</RouterLink>
        <RouterLink to="/inventory" class="nav-link">📋 Inventaire</RouterLink>
        <RouterLink to="/costs" class="nav-link">💶 Coûts</RouterLink>
        <RouterLink to="/audit" class="nav-link">🔍 Audit</RouterLink>
      </nav>
      <div class="user-widget">
        <button
          v-if="isAdmin"
          class="admin-toggle"
          :class="{ active: inAdminMode }"
          @click="toggleAdminMode"
        >
          {{ inAdminMode ? '🛡 Mode admin actif' : '⚙ Mode admin' }}
        </button>
        <span class="user-name">{{ auth.state.user.name }}</span>
        <span class="role-badge" :class="auth.state.user.role">{{ auth.state.user.role }}</span>
        <button class="btn-logout" @click="auth.logout()">Déconnexion</button>
      </div>
    </header>
    <div v-if="inAdminMode" class="admin-banner">
      <span>🛡 Vous configurez la plateforme Symphony elle-même — providers, golden paths, webhook.</span>
      <RouterLink to="/" class="admin-banner-exit">Quitter le mode admin →</RouterLink>
    </div>
    <main class="main">
      <RouterView />
    </main>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from './composables/useAuth'
const auth = useAuth()
const { isAdmin } = useAuth()
const route = useRoute()
const router = useRouter()

// Pas de nouvelle logique d'autorisation : on relit le même meta.adminOnly
// que le guard de route (router/index.js) — la bascule est un affichage,
// jamais une source de droits.
const inAdminMode = computed(() => route.meta.adminOnly === true)

function toggleAdminMode() {
  router.push(inAdminMode.value ? '/' : '/admin')
}
</script>

<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f0f2f5; color: #1a1a1a; }
.layout { display: flex; flex-direction: column; min-height: 100vh; }
.header { background: #1a1a2e; color: white; padding: 0 24px; display: flex; align-items: center; justify-content: space-between; height: 56px; position: sticky; top: 0; z-index: 100; }
.brand { display: flex; align-items: center; gap: 10px; }
.logo { font-size: 22px; }
.name { font-weight: 700; font-size: 18px; }
.subtitle { color: #666; font-size: 13px; }
.nav { display: flex; gap: 4px; }
.nav-link { color: #aaa; text-decoration: none; padding: 6px 14px; border-radius: 6px; font-size: 14px; transition: all .15s; }
.nav-link:hover { color: white; background: #ffffff15; }
.nav-link.router-link-active { color: white; background: #667eea; }
.user-widget { display: flex; align-items: center; gap: 12px; }
.user-name { font-size: 14px; color: #ccc; }
.role-badge { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 20px; text-transform: uppercase; letter-spacing: .04em; }
.role-badge.admin { background: #7c3aed22; color: #c4b5fd; border: 1px solid #7c3aed44; }
.role-badge.lead { background: #b4530022; color: #fbbf24; border: 1px solid #b4530044; }
.role-badge.developer { background: #16743322; color: #6ee7b7; border: 1px solid #16743344; }
.role-badge.viewer { background: #ffffff15; color: #888; border: 1px solid #ffffff20; }
.btn-logout { background: transparent; border: 1px solid #ffffff30; color: #aaa; border-radius: 6px; padding: 5px 12px; font-size: 13px; cursor: pointer; transition: all .15s; }
.btn-logout:hover { border-color: #ffffff60; color: white; }
.main { flex: 1; padding: 28px; max-width: 1200px; margin: 0 auto; width: 100%; }

.header.admin-mode { background: #2e1a2e; }

.admin-toggle {
  background: transparent;
  border: 1px solid #7c3aed55;
  color: #c4b5fd;
  border-radius: 6px;
  padding: 5px 12px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all .15s;
  white-space: nowrap;
}
.admin-toggle:hover { background: #7c3aed22; border-color: #7c3aed88; }
.admin-toggle.active { background: #7c3aed; border-color: #7c3aed; color: white; }
.admin-toggle.active:hover { background: #6d28d9; }

.admin-banner {
  background: #f3e8ff;
  border-bottom: 1px solid #d8b4fe;
  color: #6b21a8;
  padding: 8px 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
  gap: 12px;
}
.admin-banner-exit { color: #6b21a8; font-weight: 600; text-decoration: none; white-space: nowrap; }
.admin-banner-exit:hover { text-decoration: underline; }
</style>
