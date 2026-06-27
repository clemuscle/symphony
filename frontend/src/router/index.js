import { createRouter, createWebHistory } from 'vue-router'
import Catalogue from '../views/Catalogue.vue'
import Projects from '../views/Projects.vue'
import Deployments from '../views/Deployments.vue'
import Login from '../views/Login.vue'
import { useAuth } from '../composables/useAuth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: Login, meta: { public: true } },
    { path: '/', component: Catalogue },
    { path: '/projects', component: Projects },
    { path: '/deployments', component: Deployments },
  ]
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const { state, init } = useAuth()
  if (state.loading) await init()
  if (!state.user) return '/login'
})

export default router
