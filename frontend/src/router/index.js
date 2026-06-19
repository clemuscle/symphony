import { createRouter, createWebHistory } from 'vue-router'
import Catalogue from '../views/Catalogue.vue'
import Projects from '../views/Projects.vue'
import Deployments from '../views/Deployments.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Catalogue },
    { path: '/projects', component: Projects },
    { path: '/deployments', component: Deployments },
  ]
})
