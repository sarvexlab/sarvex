// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api, type Principal } from './client'

export async function getMe(): Promise<Principal> {
  const { data } = await api.get<Principal>('/auth/me')
  return data
}
