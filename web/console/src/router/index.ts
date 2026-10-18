// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import {
  createRouter,
  createWebHistory,
  type NavigationGuardWithThis,
  type RouteRecordRaw,
} from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useOperatorStore } from '@/stores/operator'
import { needsConnect } from './needsConnect'
import type { IconName } from '@/components/ui/Icon.vue'

export interface NavItem {
  to: string
  labelKey: string
  icon: IconName
  /** Set when `to` carries a query string — the path alone that a click can
   * land on and still count as "this section" (e.g. Catalog and Orders both
   * resolve to /store). */
  matchPath?: string
}

export const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { shell: false, public: true },
  },
  { path: '/', redirect: '/path' },
  { path: '/connect', name: 'connect', component: () => import('@/views/ConnectSurface.vue') },
  { path: '/path', name: 'path', component: () => import('@/views/PathSurface.vue') },
  { path: '/store', name: 'store', component: () => import('@/views/StoreSurface.vue') },
  // Product detail is a drawer overlay on /store (deep-link via
  // /store?product=<sku>) per the design — no standalone route.
  { path: '/health', name: 'health', component: () => import('@/views/HealthSurface.vue') },
  // Agent surfaces: one page, rendered entirely from the backend registry.
  { path: '/surfaces', name: 'surfaces', component: () => import('@/views/SurfacesSurface.vue') },
  {
    path: '/checkout-sessions/:checkoutId',
    name: 'checkout-session-detail',
    component: () => import('@/views/CheckoutSessionDetailView.vue'),
    props: true,
  },
  { path: '/analytics', name: 'analytics', component: () => import('@/views/AnalyticsSurface.vue') },
  { path: '/validation', name: 'validation', component: () => import('@/views/ValidationView.vue') },
  { path: '/account', name: 'account-home', component: () => import('@/views/AccountHomeView.vue') },
  {
    path: '/_/styleguide',
    name: 'styleguide',
    component: () => import('@/views/StyleguideView.vue'),
  },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsSurface.vue') },
  {
    path: '/board',
    name: 'board',
    component: () => import('@/views/BoardSurface.vue'),
    meta: { operator: true },
  },
  {
    path: '/board/:merchantId/:storeId',
    name: 'enter-as-merchant',
    component: () => import('@/views/EnterAsMerchantView.vue'),
    props: true,
    meta: { operator: true },
  },
]

// Exported so guard.test.ts can attach the same logic to a memory-history
// router. The production router below attaches it as a beforeEach guard.
export const beforeEachGuard: NavigationGuardWithThis<undefined> = async (to) => {
  const auth = useAuthStore()

  // Wait for the principal to land before deciding (login surface aside).
  if (auth.status === 'idle' || auth.status === 'loading') {
    await new Promise<void>((resolve) => {
      const stop = auth.$subscribe((_m, state) => {
        if (state.status !== 'idle' && state.status !== 'loading') {
          stop()
          resolve()
        }
      })
    })
  }

  if (to.meta.public) return true

  if (!auth.isAuthenticated) {
    return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : undefined }
  }

  if (to.meta.operator && !auth.isOperator) return { name: 'path' }

  // Operators outside enter-as-merchant only see the board. The merchant
  // surfaces become reachable once they've entered a merchant; on exit
  // they're routed back here.
  const operator = useOperatorStore()
  if (auth.isOperator && !operator.isImpersonating) {
    const allowed = to.path === '/board' || to.path.startsWith('/board/')
    return allowed ? true : { path: '/board' }
  }

  // Merchant pre-load (or operator-impersonating-merchant): ensure we know
  // the connector state before deciding whether to route to /connect. Both
  // lists are needed — needsConnect looks at the store↔connector linkage
  // and the connector's status. When impersonating, X-Operate-As is
  // attached automatically by the Axios layer so these fetch as the
  // target merchant.
  const stores = useStoresStore()
  const connectors = useConnectorsStore()
  await Promise.all([
    stores.loaded ? Promise.resolve() : stores.load(),
    connectors.connectors.length ? Promise.resolve() : connectors.load(),
  ])

  const requiresConnect = needsConnect(stores, connectors)
  if (requiresConnect && to.path !== '/connect') return { path: '/connect' }
  if (!requiresConnect && to.path === '/connect') return { path: '/path' }
  // The multi-store landing decision (/account vs /path) lives in
  // LoginView so it can be made once stores have loaded, after sign-in.
  // The static '/' redirect below sends people to '/path' as the default.

  return true
}

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(beforeEachGuard)

// Sidebar order (brief §Phase 2). Validation (`/validation`) folds into
// Home/Setup and stays a deep link. Health stays in the nav until its fold
// into Home lands (docs/console-redesign-plan.md §9): it is the only place
// alerts can be acknowledged/resolved, and dropping it before the fold left
// that unreachable.
export const NAV: NavItem[] = [
  { to: '/path', labelKey: 'shell.nav.home', icon: 'pulse' },
  { to: '/store?tab=products', labelKey: 'shell.nav.catalog', icon: 'store', matchPath: '/store' },
  { to: '/store?tab=orders', labelKey: 'shell.nav.orders', icon: 'receipt', matchPath: '/store' },
  { to: '/health', labelKey: 'shell.nav.health', icon: 'shield' },
  { to: '/analytics', labelKey: 'shell.nav.analytics', icon: 'zap' },
  { to: '/surfaces', labelKey: 'shell.nav.distribution', icon: 'pulse' },
  { to: '/settings', labelKey: 'shell.nav.settings', icon: 'cog' },
]
