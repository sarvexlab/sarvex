// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Account = components['schemas']['account']
type AccountUpdate = components['schemas']['account_update']
type Member = components['schemas']['member']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function getAccount(): Promise<Account> {
  const { data } = await api.get<Account>('/account')
  return data
}

export async function updateAccount(patch: AccountUpdate): Promise<Account> {
  const { data } = await api.patch<Account>('/account', patch)
  return data
}

export async function listMembers(): Promise<Member[]> {
  const { data } = await api.get<ListEnvelope<Member>>('/account/members')
  return data.data
}

export async function inviteMember(email: string): Promise<Member> {
  const { data } = await api.post<Member>('/account/members', { email })
  return data
}
