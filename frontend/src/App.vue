<template>
  <div class="layout">
    <header v-if="auth.state.user && route.path !== '/setup'" class="header">
      <div class="brand">
        <span class="logo">🎼</span>
        <span class="name">Symphony</span>
        <span class="subtitle">Internal Developer Portal</span>
      </div>
      <nav class="nav">
        <RouterLink to="/" class="nav-link">📦 Catalogue</RouterLink>
        <RouterLink to="/projects" class="nav-link">🗂 Projets</RouterLink>
        <RouterLink to="/deployments" class="nav-link">🚀 Déploiements</RouterLink>
      </nav>
      <div class="user-widget">
        <span class="user-name">{{ auth.state.user.name }}</span>
        <button class="btn-logout" @click="auth.logout()">Déconnexion</button>
      </div>
    </header>
    <main class="main">
      <RouterView />
    </main>
  </div>
</template>

<script setup>
import { useRoute } from 'vue-router'
import { useAuth } from './composables/useAuth'
const auth = useAuth()
const route = useRoute()
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
.btn-logout { background: transparent; border: 1px solid #ffffff30; color: #aaa; border-radius: 6px; padding: 5px 12px; font-size: 13px; cursor: pointer; transition: all .15s; }
.btn-logout:hover { border-color: #ffffff60; color: white; }
.main { flex: 1; padding: 28px; max-width: 1200px; margin: 0 auto; width: 100%; }
</style>
