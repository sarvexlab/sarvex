// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import { useThemeStore } from './stores/theme'
import { useAuthStore } from './stores/auth'
import { installMocks } from './api/mocks'
import './styles/fonts.css'
import './styles/tokens.css'

async function bootstrap() {
  // Always install the dual-mode adapter — it routes each request to mock
  // or real per env config (VITE_USE_MOCKS / VITE_REAL_ROUTES / VITE_MOCK_ROUTES).
  // See src/api/mocks.ts for the resolution rules.
  installMocks()

  const app = createApp(App)
  app.use(createPinia())
  app.use(i18n)

  useThemeStore().init()
  // Subscribe to the IdP and (if a token is present) prime the principal.
  // The router guard awaits this resolution before deciding the first route.
  await useAuthStore().init()

  app.use(router)
  app.mount('#app')
}

void bootstrap()
