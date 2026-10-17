<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Test\Unit\Model;

use PHPUnit\Framework\TestCase;
use Sarvex\Ucp\Model\Hmac;

/**
 * Mirrors plugin/woocommerce/tests/HMACTest.php so both platforms' request
 * signing is verified against the same expectations. Hmac has no Magento
 * dependencies, so this suite runs with a bare PHPUnit + this module's
 * autoload — no Magento install required.
 */
class HmacTest extends TestCase
{
    private Hmac $hmac;

    protected function setUp(): void
    {
        $this->hmac = new Hmac();
    }

    public function testSignProducesValidHexSignature(): void
    {
        $body = json_encode(['sku' => 'ABC123', 'entity_kind' => 'product']);

        $signature = $this->hmac->sign($body, '1686000000', 1, 'test-secret-key');

        $this->assertIsString($signature);
        $this->assertMatchesRegularExpression('/^[0-9a-f]{64}$/', $signature);
    }

    public function testSignIsDeterministic(): void
    {
        $body = json_encode(['sku' => 'TEST', 'entity_kind' => 'inventory']);

        $sig1 = $this->hmac->sign($body, '1686000000', 5, 'my-secret');
        $sig2 = $this->hmac->sign($body, '1686000000', 5, 'my-secret');

        $this->assertSame($sig1, $sig2);
    }

    public function testSignDifferentBodyProducesDifferentSignature(): void
    {
        $sig1 = $this->hmac->sign('body1', '1686000000', 1, 'secret');
        $sig2 = $this->hmac->sign('body2', '1686000000', 1, 'secret');

        $this->assertNotSame($sig1, $sig2);
    }

    public function testSignDifferentTimestampProducesDifferentSignature(): void
    {
        $sig1 = $this->hmac->sign('test-body', '1686000000', 1, 'secret');
        $sig2 = $this->hmac->sign('test-body', '1686000001', 1, 'secret');

        $this->assertNotSame($sig1, $sig2);
    }

    public function testSignDifferentSequenceProducesDifferentSignature(): void
    {
        $sig1 = $this->hmac->sign('test-body', '1686000000', 1, 'secret');
        $sig2 = $this->hmac->sign('test-body', '1686000000', 2, 'secret');

        $this->assertNotSame($sig1, $sig2);
    }

    public function testHeadersReturnsRequiredFields(): void
    {
        $headers = $this->hmac->headers('store-123', 'conn-abc', 'abc123def456', '1686000000', 1);

        $this->assertArrayHasKey('X-Sarvex-Credential', $headers);
        $this->assertArrayHasKey('X-Sarvex-Timestamp', $headers);
        $this->assertArrayHasKey('X-Sarvex-Sequence', $headers);
        $this->assertArrayHasKey('X-Sarvex-Signature', $headers);
    }

    public function testHeadersFormatsCredentialCorrectly(): void
    {
        $headers = $this->hmac->headers('store-xyz', 'conn-123', 'sig', '1000', 5);

        $this->assertSame('store-xyz:conn-123', $headers['X-Sarvex-Credential']);
        $this->assertSame('1000', $headers['X-Sarvex-Timestamp']);
        $this->assertSame(5, $headers['X-Sarvex-Sequence']);
        $this->assertSame('sig', $headers['X-Sarvex-Signature']);
    }

    public function testEmptyBodyProducesValidSignature(): void
    {
        $signature = $this->hmac->sign('', '1000', 0, 'secret');

        $this->assertIsString($signature);
        $this->assertMatchesRegularExpression('/^[0-9a-f]{64}$/', $signature);
    }
}
