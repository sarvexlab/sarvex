// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import {
  getNotificationPreferences,
  updateNotificationPreferences,
} from '@/api/notifications'
import type { components } from '@/api/schema'

type Prefs = components['schemas']['notification_preferences']

export const useNotificationsStore = defineStore('notifications', {
  state: () => ({
    prefs: null as Prefs | null,
    loading: false,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        this.prefs = await getNotificationPreferences()
      } finally {
        this.loading = false
      }
    },
    async save(prefs: Prefs) {
      this.prefs = await updateNotificationPreferences(prefs)
    },
  },
})
