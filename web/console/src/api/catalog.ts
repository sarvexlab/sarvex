// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { api } from './client'
import type { components } from './schema'

type Readiness = components['schemas']['readiness']
type ProductSummary = components['schemas']['product_summary']
type ProductDetail = components['schemas']['product_detail']
type ListEnvelope<T> = { data: T[]; page: { cursor: string | null; has_more: boolean } }

export async function getReadiness(storeId: string): Promise<Readiness> {
  const { data } = await api.get<Readiness>(
    `/stores/${encodeURIComponent(storeId)}/catalog/readiness`,
  )
  return data
}

export type ProductPage = { products: ProductSummary[]; cursor: string | null; hasMore: boolean }

export async function listProducts(
  storeId: string,
  opts: { minScore?: number; hasBlockingIssue?: boolean; cursor?: string } = {},
): Promise<ProductPage> {
  const { data } = await api.get<ListEnvelope<ProductSummary>>(
    `/stores/${encodeURIComponent(storeId)}/catalog/products`,
    {
      params: {
        min_score: opts.minScore,
        has_blocking_issue: opts.hasBlockingIssue,
        cursor: opts.cursor,
      },
    },
  )
  return { products: data.data, cursor: data.page.cursor, hasMore: data.page.has_more }
}

export async function getProduct(storeId: string, sku: string): Promise<ProductDetail> {
  const { data } = await api.get<ProductDetail>(
    `/stores/${encodeURIComponent(storeId)}/catalog/products/${encodeURIComponent(sku)}`,
  )
  return data
}
