// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

export type Activation = components['schemas']['activation']
export type ActivationStep = components['schemas']['activation_step']
export type UCPState = components['schemas']['ucp_state']
export type VerificationReport = components['schemas']['verification_report']

/** The store's own activation state: profile served, capabilities declared,
 * signatures verifying, checkout working. Nothing here references an agent
 * surface — being live on Google is a separate, optional enrollment. */
export async function getActivation(storeId: string): Promise<Activation> {
  const { data } = await api.get<Activation>(
    `/stores/${encodeURIComponent(storeId)}/activation`,
  )
  return data
}

/** Runs the conformance suite against the store's own endpoints. A run that
 * finds failures resolves normally with `passed: false` — it is a successful
 * request reporting problems, not an error. */
export async function verifyActivation(storeId: string): Promise<VerificationReport> {
  const { data } = await api.post<VerificationReport>(
    `/stores/${encodeURIComponent(storeId)}/activation/verify`,
  )
  return data
}
