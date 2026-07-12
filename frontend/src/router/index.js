import { createRouter, createWebHistory } from 'vue-router'
import Catalogue from '../views/Catalogue.vue'
import Projects from '../views/Projects.vue'
import Deployments from '../views/Deployments.vue'
import Inventory from '../views/Inventory.vue'
import Costs from '../views/Costs.vue'
import Audit from '../views/Audit.vue'
import Login from '../views/Login.vue'
import Setup from '../views/Setup.vue'
import { useAuth } from '../composables/useAuth'
import { api } from '../api'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: Login, meta: { public: true } },
    { path: '/setup', component: Setup, meta: { skipSetupCheck: true } },
    { path: '/', component: Catalogue },
    { path: '/projects', component: Projects },
    { path: '/deployments', component: Deployments },
    { path: '/inventory', component: Inventory },
    { path: '/costs', component: Costs },
    { path: '/audit', component: Audit },
  ]
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true

  // Vérification de l'authentification
  const { state, init } = useAuth()
  if (state.loading) await init()
  if (!state.user) return '/login'

  // Vérification du setup (sauf sur /setup lui-même)
  if (!to.meta.skipSetupCheck) {
    try {
      const r = await api.getSetupStatus()
      if (!r.data.configured) return '/setup'
    } catch {
      // En cas d'erreur réseau on laisse passer — l'app affichera les erreurs provider
    }
  }
})

export default router
