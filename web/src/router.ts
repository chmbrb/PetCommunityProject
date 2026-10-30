// SPDX-License-Identifier: AGPL-3.0-or-later
import { createRouter, createWebHistory } from 'vue-router'
import HomeView from './views/HomeView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/signup', name: 'signup', component: () => import('./views/SignUpView.vue') },
    { path: '/signin', name: 'signin', component: () => import('./views/SignInView.vue') },
    { path: '/signin/email', name: 'signin-email', component: () => import('./views/EmailSignInView.vue') },
    { path: '/verify-email', name: 'verify-email', component: () => import('./views/VerifyEmailView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue') },
  ],
})
