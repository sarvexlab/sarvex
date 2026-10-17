<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

use Magento\Framework\HTTP\Client\Curl;
use Psr\Log\LoggerInterface;

/**
 * Sends lean, id-only change signals to Sarvex — CLAUDE.md §4 Role 1
 * ("thin events, fat pull"): Sarvex pulls the full entity itself in
 * response. Signals carry only a SKU and entity kind, never product data,
 * PII, or payment credentials. A failed send is logged and swallowed
 * (never thrown), matching the WooCommerce plugin's SignalSender — a
 * Sarvex outage must never block a merchant's product save, and a dropped
 * signal is caught by the sync-plane's reconciliation sweeper.
 */
class SignalSender
{
    public function __construct(
        private readonly Curl $httpClient,
        private readonly Config $config,
        private readonly Hmac $hmac,
        private readonly LoggerInterface $logger
    ) {
    }

    public function sendProductChanged(string $sku): void
    {
        $this->send(['sku' => $sku, 'entity_kind' => 'product']);
    }

    public function sendInventoryChanged(string $sku): void
    {
        $this->send(['sku' => $sku, 'entity_kind' => 'inventory']);
    }

    /**
     * @param array<string, string> $signal
     */
    private function send(array $signal): void
    {
        if (!$this->config->isPaired()) {
            $this->logger->debug('sarvex_ucp: skipping signal, pairing not complete', $signal);
            return;
        }

        $sequence = $this->config->nextSequence('signal');
        $timestamp = (string)time();
        $body = json_encode($signal, JSON_THROW_ON_ERROR);
        $signature = $this->hmac->sign($body, $timestamp, $sequence, $this->config->getHmacSecret());

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
            $this->httpClient->post(rtrim($this->config->getEndpointUrl(), '/') . '/connector/signals', $body);
            $this->logger->debug('sarvex_ucp: signal sent', ['signal' => $signal, 'sequence' => $sequence]);
        } catch (\Throwable $e) {
            $this->logger->warning('sarvex_ucp: failed to send signal', [
                'error' => $e->getMessage(),
                'signal' => $signal,
            ]);
        }
    }
}
