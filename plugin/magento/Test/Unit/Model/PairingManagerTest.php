<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Test\Unit\Model;

use Magento\Framework\HTTP\Client\Curl;
use PHPUnit\Framework\TestCase;
use Psr\Log\LoggerInterface;
use Sarvex\Ucp\Model\Config;
use Sarvex\Ucp\Model\Hmac;
use Sarvex\Ucp\Model\PairingManager;

/**
 * Mirrors plugin/woocommerce/tests/PairingManagerTest.php's coverage.
 * PairingManager's dependencies are constructor-injected (Curl, Config,
 * Hmac, LoggerInterface) rather than read from WordPress-style globals, so
 * the parameter-validation and HMAC-signing paths below are fully mockable
 * with plain PHPUnit. Running this suite still requires a Magento dev
 * environment (composer require magento/framework, for Curl/LoggerInterface
 * to resolve) — not available in every environment this module ships to,
 * matching the WooCommerce plugin's own tests, which equally require a
 * WordPress test harness (WP_Mock/Brain Monkey) not bundled in this repo.
 */
class PairingManagerTest extends TestCase
{
    private const TEST_ENDPOINT = 'http://localhost:8080';
    private const TEST_STORE_ID = 'store-abc123';
    private const TEST_CONNECTOR_ID = 'conn-def456';
    private const TEST_PAIRING_CODE = 'pair-ghi789';

    private Curl $httpClient;
    private Config $config;
    private LoggerInterface $logger;
    private PairingManager $manager;

    protected function setUp(): void
    {
        $this->httpClient = $this->createMock(Curl::class);
        $this->config = $this->createMock(Config::class);
        $this->logger = $this->createMock(LoggerInterface::class);
        $this->manager = new PairingManager($this->httpClient, $this->config, new Hmac(), $this->logger);
    }

    // ── Parameter validation ────────────────────────────────────────────────

    public function testCompletePairingRejectsEmptyEndpoint(): void
    {
        $this->httpClient->expects($this->never())->method('post');

        $result = $this->manager->completePairing('', self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }

    public function testCompletePairingRejectsEmptyStoreId(): void
    {
        $result = $this->manager->completePairing(self::TEST_ENDPOINT, '', self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }

    public function testCompletePairingRejectsEmptyConnectorId(): void
    {
        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, '', self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }

    public function testCompletePairingRejectsEmptyPairingCode(): void
    {
        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, '');

        $this->assertFalse($result);
    }

    public function testCompletePairingLogsMissingParameters(): void
    {
        $this->logger->expects($this->once())->method('warning')->with(
            $this->stringContains('missing parameters')
        );

        $this->manager->completePairing('', self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, '');
    }

    public function testBeginPairingRejectsMissingBearerToken(): void
    {
        $this->httpClient->expects($this->never())->method('post');

        $result = $this->manager->beginPairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, '');

        $this->assertNull($result);
    }

    // ── HMAC request signing ────────────────────────────────────────────────

    public function testCompletePairingRequestIncludesHmacHeaders(): void
    {
        $hmac = new Hmac();
        $body = json_encode(['connector_id' => self::TEST_CONNECTOR_ID, 'pairing_code' => self::TEST_PAIRING_CODE]);
        $timestamp = gmdate('c');
        $sequence = 1;

        $signature = $hmac->sign($body, $timestamp, $sequence, self::TEST_PAIRING_CODE);
        $headers = $hmac->headers(self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, $signature, $timestamp, $sequence);

        $this->assertSame(self::TEST_STORE_ID . ':' . self::TEST_CONNECTOR_ID, $headers['X-Sarvex-Credential']);
        $this->assertMatchesRegularExpression('/^[a-f0-9]{64}$/', $headers['X-Sarvex-Signature']);
    }

    public function testHmacSignatureChangesWithDifferentPairingCode(): void
    {
        $hmac = new Hmac();
        $body = json_encode(['connector_id' => self::TEST_CONNECTOR_ID, 'pairing_code' => self::TEST_PAIRING_CODE]);
        $timestamp = gmdate('c');

        $sig1 = $hmac->sign($body, $timestamp, 1, self::TEST_PAIRING_CODE);
        $sig2 = $hmac->sign($body, $timestamp, 1, 'different-code');

        $this->assertNotSame($sig1, $sig2, 'Signature should change with different pairing code');
    }

    // ── Credential storage ──────────────────────────────────────────────────

    public function testCompletePairingStoresConnectorCredentialsOnSuccess(): void
    {
        $this->httpClient->method('getStatus')->willReturn(200);
        $this->httpClient->method('getBody')->willReturn(json_encode([
            'connector_id' => self::TEST_CONNECTOR_ID,
            'store_id' => self::TEST_STORE_ID,
        ]));

        $this->config->expects($this->once())
            ->method('savePairing')
            ->with(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertTrue($result);
    }

    // ── Error handling ──────────────────────────────────────────────────────

    public function testCompletePairingHandlesHttpTransportError(): void
    {
        $this->httpClient->method('post')->willThrowException(new \RuntimeException('connection refused'));
        $this->logger->expects($this->once())->method('error')->with($this->stringContains('pairing request failed'), $this->anything());

        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }

    public function testCompletePairingHandlesNonSuccessStatus(): void
    {
        $this->httpClient->method('getStatus')->willReturn(401);
        $this->httpClient->method('getBody')->willReturn('{"error":"invalid pairing code"}');

        $this->config->expects($this->never())->method('savePairing');

        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }

    public function testCompletePairingHandlesResponseMissingRequiredFields(): void
    {
        $this->httpClient->method('getStatus')->willReturn(200);
        $this->httpClient->method('getBody')->willReturn('{}');

        $this->config->expects($this->never())->method('savePairing');

        $result = $this->manager->completePairing(self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE);

        $this->assertFalse($result);
    }
}
