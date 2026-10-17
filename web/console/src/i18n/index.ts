// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// vue-i18n instance. Composition-API mode (legacy: false) so components use
// useI18n()'s `t` rather than the Options-API `this.$t`, matching this
// codebase's <script setup> convention everywhere else.
//
// Messages are namespaced per feature (`en.ledger.*`, …) in src/locales so
// unrelated surfaces don't collide as more of the console migrates onto
// vue-i18n. Only `en` exists today — add a locale by adding a file here and
// registering it below.

import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import { surfaceMessages } from '@/surfaces'

export const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  // Each surface's copy is its own module (src/surfaces/<id>/locale.en.ts),
  // merged under `surfaces.<id>` so a new surface edits no shared file.
  messages: { en: { ...en, surfaces: { ...en.surfaces, ...surfaceMessages } } },
})
