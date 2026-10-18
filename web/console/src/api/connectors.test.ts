// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// The Connect page's unit test mocks createConnector() at the module
// boundary, so it never sees the URL — which is how pairing ended up posting
// to POST /connectors (405 against the control plane). Pin the request here.

import { describe, it, expect, vi, afterEach } from 'vitest'
import { api } from './client'
import { createConnector } from './connectors'

afterEach(() => vi.restoreAllMocks())

describe('createConnector', () => {
  it('posts { platform } to the store-scoped pairing route', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { connector_id: 'con-1', pairing_code: 'P-1', store_id: 'str/1' } })
    const res = await createConnector('str/1', 'woocommerce')
    expect(post).toHaveBeenCalledWith('/stores/str%2F1/connectors/pairing', { platform: 'woocommerce' })
    expect(res.pairing_code).toBe('P-1')
  })
})
