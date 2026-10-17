/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE?: string
  readonly VITE_USE_MOCKS?: string
  readonly VITE_REAL_ROUTES?: string
  readonly VITE_MOCK_ROUTES?: string
  readonly VITE_API_DEBUG?: string
  readonly VITE_AUTH_MODE?: 'simulator' | 'kinde'
  readonly VITE_KINDE_DOMAIN?: string
  readonly VITE_KINDE_CLIENT_ID?: string
  readonly VITE_KINDE_REDIRECT_URI?: string
  readonly VITE_KINDE_LOGOUT_URI?: string
  readonly VITE_KINDE_AUDIENCE?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}
