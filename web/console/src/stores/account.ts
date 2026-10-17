// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia'
import { getAccount, inviteMember, listMembers, updateAccount } from '@/api/account'
import type { components } from '@/api/schema'

type Account = components['schemas']['account']
type AccountUpdate = components['schemas']['account_update']
type Member = components['schemas']['member']

export const useAccountStore = defineStore('account', {
  state: () => ({
    account: null as Account | null,
    members: [] as Member[],
    loading: false,
    loaded: false,
    error: null as string | null,
  }),
  actions: {
    async load() {
      this.loading = true
      this.error = null
      try {
        const [a, m] = await Promise.all([getAccount(), listMembers()])
        this.account = a
        this.members = m
        this.loaded = true
      } catch (e: unknown) {
        this.error = e instanceof Error ? e.message : 'Failed to load account'
      } finally {
        this.loading = false
      }
    },
    async save(patch: AccountUpdate) {
      this.account = await updateAccount(patch)
    },
    async invite(email: string) {
      const member = await inviteMember(email)
      this.members.push(member)
      return member
    },
  },
})
