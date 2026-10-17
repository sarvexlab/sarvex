// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

export type SurfaceDescriptor = components['schemas']['surface_descriptor']
export type SurfaceEnrollment = components['schemas']['surface_enrollment']
export type SurfaceState = components['schemas']['surface_state']
export type PrerequisiteResult = components['schemas']['prerequisite_result']
export type SurfaceArtifact = components['schemas']['surface_artifact']
export type SurfaceExternalStep = components['schemas']['surface_external_step']

type ListEnvelope<T> = { data: T[]; page: components['schemas']['page'] }

/** One entry per registered surface, including ones the store has never
 * touched (reported as `available`). Each carries its descriptor, which is
 * what lets a single generic component render every surface's flow. */
export async function listStoreSurfaces(storeId: string): Promise<SurfaceEnrollment[]> {
  const { data } = await api.get<ListEnvelope<SurfaceEnrollment>>(
    `/stores/${encodeURIComponent(storeId)}/surfaces`,
  )
  return data.data
}

export async function enrollSurface(
  storeId: string,
  surfaceId: string,
): Promise<SurfaceEnrollment> {
  const { data } = await api.post<SurfaceEnrollment>(
    `/stores/${encodeURIComponent(storeId)}/surfaces/${encodeURIComponent(surfaceId)}/enroll`,
  )
  return data
}

export async function evaluatePrerequisites(
  storeId: string,
  surfaceId: string,
): Promise<SurfaceEnrollment> {
  const { data } = await api.post<SurfaceEnrollment>(
    `/stores/${encodeURIComponent(storeId)}/surfaces/${encodeURIComponent(surfaceId)}/prerequisites/evaluate`,
  )
  return data
}

export async function submitSurface(
  storeId: string,
  surfaceId: string,
): Promise<SurfaceEnrollment> {
  const { data } = await api.post<SurfaceEnrollment>(
    `/stores/${encodeURIComponent(storeId)}/surfaces/${encodeURIComponent(surfaceId)}/submit`,
  )
  return data
}

/** Per-surface configuration is an untyped string map by design: its shape
 * is the surface's own business, and typing it here would put
 * surface-specific knowledge into the generic layer. */
export async function setSurfaceConfig(
  storeId: string,
  surfaceId: string,
  config: Record<string, string>,
): Promise<SurfaceEnrollment> {
  const { data } = await api.put<SurfaceEnrollment>(
    `/stores/${encodeURIComponent(storeId)}/surfaces/${encodeURIComponent(surfaceId)}/config`,
    { config },
  )
  return data
}

/** Withdraws enrollment. Does not affect the store's activation state, and
 * does not affect any other surface. */
export async function withdrawSurface(storeId: string, surfaceId: string): Promise<void> {
  await api.delete(
    `/stores/${encodeURIComponent(storeId)}/surfaces/${encodeURIComponent(surfaceId)}`,
  )
}
