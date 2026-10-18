// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The commerce backends a store can run on, as the control-plane contract
// enumerates them (components['schemas']['platform']).
//
// STORE_PLATFORMS is a Record over the contract's enum: adding a platform to
// sarvex-v1-control-plane.yaml and regenerating schema.d.ts (npm run gen:api)
// fails typecheck here until the new platform has its label
// (docs/HOWTO-add-platform.md).

import type { components } from '@/api/schema'

export type StorePlatform = components['schemas']['platform']

// The i18n key of each platform's display name, in the order the console
// offers them; the first is the default.
export const STORE_PLATFORMS: Record<StorePlatform, string> = {
  woocommerce: 'settings.store.woocommerce',
  magento: 'settings.store.magento',
}

export const DEFAULT_STORE_PLATFORM = Object.keys(STORE_PLATFORMS)[0] as StorePlatform

// platformLabelKey is the i18n key of a store's platform name, falling back to
// the default platform's for a store with none recorded.
export function platformLabelKey(p: string | undefined): string {
  return STORE_PLATFORMS[(p ?? DEFAULT_STORE_PLATFORM) as StorePlatform] ?? STORE_PLATFORMS[DEFAULT_STORE_PLATFORM]
}
