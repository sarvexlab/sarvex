// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Account = components['schemas']['account']
type BoardCard = components['schemas']['board_card']
type Member = components['schemas']['member']
type MemberInvite = components['schemas']['member_invite']
type Store = components['schemas']['store']
type Platform = components['schemas']['platform']
type ManualStep = components['schemas']['manual_step']
type AuditEntry = components['schemas']['audit_entry']
type JourneyState = components['schemas']['journey_state']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function listBoard(filter: { state?: JourneyState; attention?: boolean } = {}): Promise<BoardCard[]> {
  const { data } = await api.get<ListEnvelope<BoardCard>>('/operator/board', { params: filter })
  return data.data
}

export async function listMerchants(): Promise<Account[]> {
  const { data } = await api.get<ListEnvelope<Account>>('/operator/merchants')
  return data.data
}

export async function createMerchant(name: string, ownerEmail: string, businessFacts?: Record<string, unknown>): Promise<Account> {
  const { data } = await api.post<Account>('/operator/merchants', {
    name,
    owner_email: ownerEmail,
    business_facts: businessFacts,
  })
  return data
}

export async function inviteMember(merchantId: string, invite: MemberInvite): Promise<Member> {
  const { data } = await api.post<Member>(
    `/operator/merchants/${encodeURIComponent(merchantId)}/members`,
    invite,
  )
  return data
}

export async function listMerchantStores(merchantId: string): Promise<Store[]> {
  const { data } = await api.get<ListEnvelope<Store>>(
    `/operator/merchants/${encodeURIComponent(merchantId)}/stores`,
  )
  return data.data
}

export async function createMerchantStore(
  merchantId: string,
  body: { platform: Platform; name: string },
): Promise<Store> {
  const { data } = await api.post<Store>(
    `/operator/merchants/${encodeURIComponent(merchantId)}/stores`,
    body,
  )
  return data
}

export async function logManualStep(step: ManualStep): Promise<ManualStep> {
  const { data } = await api.post<ManualStep>('/operator/manual-steps', step)
  return data
}

export async function listAuditLog(targetMerchantId?: string): Promise<AuditEntry[]> {
  const { data } = await api.get<ListEnvelope<AuditEntry>>('/operator/audit-log', {
    params: targetMerchantId ? { target_merchant_id: targetMerchantId } : {},
  })
  return data.data
}
