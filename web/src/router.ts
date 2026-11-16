// SPDX-License-Identifier: AGPL-3.0-or-later
import { createRouter, createWebHistory } from 'vue-router'
import { connectedInstance, isApp } from './appInstance'
import HomeView from './views/HomeView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    // The apps' instance picker (ADR 0023). The web app is its own instance.
    { path: '/instance', name: 'instance', component: () => import('./views/InstanceView.vue') },
    { path: '/signup', name: 'signup', component: () => import('./views/SignUpView.vue') },
    { path: '/signin', name: 'signin', component: () => import('./views/SignInView.vue') },
    {
      path: '/signin/email',
      name: 'signin-email',
      component: () => import('./views/EmailLinkView.vue'),
      props: { purpose: 'signin' },
    },
    {
      path: '/signup/email',
      name: 'signup-email',
      component: () => import('./views/EmailLinkView.vue'),
      props: { purpose: 'signup' },
    },
    { path: '/verify-email', name: 'verify-email', component: () => import('./views/VerifyEmailView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue') },
  ],
})

router.beforeEach((to) => {
  if (!isApp) {
    // The picker has no meaning on the web.
    return to.name === 'instance' ? { name: 'home' } : true
  }
  // Until an instance is chosen, the app can only show the picker.
  if (!connectedInstance() && to.name !== 'instance') {
    return { name: 'instance' }
  }
  return true
})

export default router
