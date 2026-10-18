// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'

export type Theme = 'paper' | 'midnight'
const STORAGE_KEY = 'sarvex.theme'

function apply(theme: Theme) {
  // Match the design source: tokens are scoped to body[data-theme="…"]
  // so component CSS can read var(--…) the same way the mockup does.
  document.body.setAttribute('data-theme', theme)
}

export const useThemeStore = defineStore('theme', {
  // Dark ("midnight") is the product default, matching the marketing site —
  // not conditioned on OS preference. A stored explicit choice always wins.
  state: () => ({ theme: 'midnight' as Theme }),
  actions: {
    init() {
      const stored = localStorage.getItem(STORAGE_KEY) as Theme | null
      this.theme = stored ?? 'midnight'
      apply(this.theme)
    },
    set(theme: Theme) {
      this.theme = theme
      localStorage.setItem(STORAGE_KEY, theme)
      apply(theme)
    },
    toggle() {
      this.set(this.theme === 'paper' ? 'midnight' : 'paper')
    },
  },
})
