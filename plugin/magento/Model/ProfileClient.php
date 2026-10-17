<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

use Magento\Framework\HTTP\Client\Curl;
use Psr\Log\LoggerInterface;

/**
 * Fetches the merchant's UCP profile from Sarvex via GET /connector/profile,
 * HMAC-signed the same way as SignalSender. Returns null (never throws) on
 * any failure so the caller (Controller\Wellknown\Ucp) can fall back to the
 * last-known-good ProfileCache, matching ProfileProxy::fetch_profile_from_sarvex
 * in the WooCommerce plugin.
 */
class ProfileClient
{
    public function __construct(
        private readonly Curl $httpClient,
        private readonly Config $config,
        private readonly Hmac $hmac,
        private readonly LoggerInterface $logger
    ) {
    }

    public function fetch(): ?string
    {
        if (!$this->config->isPaired()) {
            return null;
        }

        $sequence = $this->config->nextSequence('profile');
        $timestamp = (string)time();
        $signature = $this->hmac->sign('', $timestamp, $sequence, $this->config->getHmacSecret());

        $this->httpClient->setTimeout(5);
        $this->httpClient->setHeaders(array_merge(
            ['Content-Type' => 'application/json'],
            $this->hmac->headers(
                $this->config->getStoreId(),
                $this->config->getConnectorId(),
                $signature,
                $timestamp,
                $sequence
            )
        ));

        try {
            $this->httpClient->get(rtrim($this->config->getEndpointUrl(), '/') . '/connector/profile');
        } catch (\Throwable $e) {
            $this->logger->warning('sarvex_ucp: failed to fetch profile from Sarvex', ['error' => $e->getMessage()]);
            return null;
        }

        $status = $this->httpClient->getStatus();
        if ($status !== 200) {
            $this->logger->warning('sarvex_ucp: Sarvex returned non-200 for profile', ['status' => $status]);
            return null;
        }

        return $this->httpClient->getBody();
    }
}
