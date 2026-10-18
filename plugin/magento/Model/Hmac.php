<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

/**
 * HMAC-SHA256 request signing, matching the WooCommerce plugin's HMAC class
 * (plugin/woocommerce/src/HMAC.php) byte-for-byte — message = body .
 * timestamp . sequence — so Sarvex's connector-ingest signature
 * verification is identical across platforms.
 */
class Hmac
{
    public function sign(string $body, string $timestamp, int $sequence, string $secret): string
    {
        return hash_hmac('sha256', $body . $timestamp . $sequence, $secret, false);
    }

    /**
     * @return array<string, int|string>
     */
    public function headers(string $storeId, string $connectorId, string $signature, string $timestamp, int $sequence): array
    {
        return [
            'X-Sarvex-Credential' => "$storeId:$connectorId",
            'X-Sarvex-Timestamp' => $timestamp,
            'X-Sarvex-Sequence' => $sequence,
            'X-Sarvex-Signature' => $signature,
        ];
    }
}
