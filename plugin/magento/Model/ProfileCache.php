<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

use Magento\Framework\App\CacheInterface;

/**
 * Last-known-good profile cache backing the /.well-known/ucp doorway's
 * resilience fallback (sarvex-v1-architecture.md: served with a plain 200 even if
 * Sarvex is briefly unreachable). This is a resilience cache, not durable
 * storage — losing it on a cache flush just means one degraded fetch,
 * unlike the signal sequence counters in Config, which must survive one.
 */
class ProfileCache
{
    private const CACHE_ID = 'sarvex_ucp_profile';
    private const CACHE_TAG = 'SARVEX_UCP';
    private const TTL_SECONDS = 3600;

    public function __construct(private readonly CacheInterface $cache)
    {
    }

    public function load(): ?string
    {
        $value = $this->cache->load(self::CACHE_ID);
        return $value === false ? null : $value;
    }

    public function save(string $profile): void
    {
        $this->cache->save($profile, self::CACHE_ID, [self::CACHE_TAG], self::TTL_SECONDS);
    }
}
