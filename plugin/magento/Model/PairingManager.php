<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

use Magento\Framework\HTTP\Client\Curl;
use Psr\Log\LoggerInterface;

/**
 * Pairs this store with Sarvex, matching plugin/woocommerce/src/PairingManager.php:
 * beginPairing requests a pairing code from the Sarvex console (bearer-
 * authenticated); completePairing exchanges that code for durable connector
 * credentials, HMAC-signed using the pairing code itself as the one-time
 * secret for this handshake request.
 */
class PairingManager
{
    public function __construct(
        private readonly Curl $httpClient,
        private readonly Config $config,
        private readonly Hmac $hmac,
        private readonly LoggerInterface $logger
    ) {
    }

    /**
     * Begin pairing: request a pairing code from Sarvex for this store.
     *
     * @return array{connector_id: string, pairing_code: string, store_id: string}|null
     */
    public function beginPairing(string $endpointUrl, string $storeId, string $bearerToken): ?array
    {
        if ($endpointUrl === '' || $storeId === '' || $bearerToken === '') {
            $this->logger->warning('sarvex_ucp: begin pairing attempted with missing parameters');
            return null;
        }

        $this->httpClient->setTimeout(5);
        $this->httpClient->setHeaders([
            'Content-Type' => 'application/json',
            'Authorization' => 'Bearer ' . $bearerToken,
        ]);

        try {
            $this->httpClient->post(
                rtrim($endpointUrl, '/') . '/v1/stores/' . rawurlencode($storeId) . '/connectors/pairing',
                json_encode(['platform' => 'magento'], JSON_THROW_ON_ERROR)
            );
        } catch (\Throwable $e) {
            $this->logger->error('sarvex_ucp: begin pairing request failed', ['error' => $e->getMessage()]);
            return null;
        }

        if ($this->httpClient->getStatus() !== 201) {
            $this->logger->error('sarvex_ucp: begin pairing failed', [
                'status' => $this->httpClient->getStatus(),
                'body' => $this->httpClient->getBody(),
            ]);
            return null;
        }

        $data = json_decode((string)$this->httpClient->getBody(), true);
        $connectorId = $data['connector']['id'] ?? null;
        if (empty($data['pairing_code']) || empty($data['store_id']) || empty($connectorId)) {
            $this->logger->error('sarvex_ucp: begin pairing response missing required fields', [
                'body' => $this->httpClient->getBody(),
            ]);
            return null;
        }

        return [
            'connector_id' => (string)$connectorId,
            'pairing_code' => (string)$data['pairing_code'],
            'store_id' => (string)$data['store_id'],
        ];
    }

    /**
     * Complete pairing: exchange the pairing code + connector_id for durable
     * connector credentials, and persist them via Config::savePairing.
     */
    public function completePairing(string $endpointUrl, string $storeId, string $connectorId, string $pairingCode): bool
    {
        if ($endpointUrl === '' || $storeId === '' || $connectorId === '' || $pairingCode === '') {
            $this->logger->warning('sarvex_ucp: pairing attempt with missing parameters');
            return false;
        }

        $body = json_encode(['connector_id' => $connectorId, 'pairing_code' => $pairingCode], JSON_THROW_ON_ERROR);
        $sequence = 1;
        $timestamp = gmdate('c');
        $signature = $this->hmac->sign($body, $timestamp, $sequence, $pairingCode);

        $this->httpClient->setTimeout(5);
        $this->httpClient->setHeaders(array_merge(
            ['Content-Type' => 'application/json'],
            $this->hmac->headers($storeId, $connectorId, $signature, $timestamp, $sequence)
        ));

        try {
            $this->httpClient->post(rtrim($endpointUrl, '/') . '/v1/connectors/pairing/complete', $body);
        } catch (\Throwable $e) {
            $this->logger->error('sarvex_ucp: pairing request failed', [
                'error' => $e->getMessage(),
                'connector_id' => $connectorId,
            ]);
            return false;
        }

        $status = $this->httpClient->getStatus();
        if ($status !== 200 && $status !== 201) {
            $this->logger->error('sarvex_ucp: pairing failed', [
                'status' => $status,
                'body' => $this->httpClient->getBody(),
                'connector_id' => $connectorId,
            ]);
            return false;
        }

        $data = json_decode((string)$this->httpClient->getBody(), true);
        if (empty($data['connector_id']) || empty($data['store_id'])) {
            $this->logger->error('sarvex_ucp: pairing response missing required fields', [
                'body' => $this->httpClient->getBody(),
            ]);
            return false;
        }

        $this->config->savePairing($endpointUrl, (string)$data['store_id'], (string)$data['connector_id'], $pairingCode);

        $this->logger->info('sarvex_ucp: pairing completed successfully', [
            'connector_id' => $data['connector_id'],
            'store_id' => $data['store_id'],
        ]);

        return true;
    }

    public function isPaired(): bool
    {
        return $this->config->isPaired();
    }
}
