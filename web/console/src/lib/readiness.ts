// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Plain-English labels for the readiness dimensions — the API names them as
// snake_case keys. Shared by the Products tab's store-level summary and the
// product drawer's per-product breakdown so both name an area the same way.

const DIM_LABELS: Record<string, string> = {
  completeness: 'Complete listings',
  consistency: 'Consistent data',
  attribute_quality: 'Attribute quality',
  delivery_data: 'Delivery info',
}

export function dimLabel(key: string): string {
  return DIM_LABELS[key] ?? key.replace(/_/g, ' ')
}
