// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Prefs = components['schemas']['notification_preferences']

export async function getNotificationPreferences(): Promise<Prefs> {
  const { data } = await api.get<Prefs>('/notifications/preferences')
  return data
}

export async function updateNotificationPreferences(prefs: Prefs): Promise<Prefs> {
  const { data } = await api.put<Prefs>('/notifications/preferences', prefs)
  return data
}
