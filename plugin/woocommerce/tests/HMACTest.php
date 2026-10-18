<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

use PHPUnit\Framework\TestCase;
use Sarvex\UCP\HMAC;

class HMACTest extends TestCase {
    public function test_sign_request_produces_valid_hmac() {
        $body = json_encode(['sku' => 'ABC123', 'entity_kind' => 'product']);
        $timestamp = '1686000000';
        $sequence = 1;
        $secret = 'test-secret-key';

        $signature = HMAC::sign_request($body, $timestamp, $sequence, $secret);

        // Verify signature is hex string
        $this->assertIsString($signature);
        $this->assertMatchesRegularExpression('/^[0-9a-f]{64}$/', $signature);
    }

    public function test_sign_request_is_deterministic() {
        $body = json_encode(['sku' => 'TEST', 'entity_kind' => 'inventory']);
        $timestamp = '1686000000';
        $sequence = 5;
        $secret = 'my-secret';

        $sig1 = HMAC::sign_request($body, $timestamp, $sequence, $secret);
        $sig2 = HMAC::sign_request($body, $timestamp, $sequence, $secret);

        $this->assertEquals($sig1, $sig2);
    }

    public function test_sign_request_different_body_produces_different_signature() {
        $timestamp = '1686000000';
        $sequence = 1;
        $secret = 'secret';

        $sig1 = HMAC::sign_request('body1', $timestamp, $sequence, $secret);
        $sig2 = HMAC::sign_request('body2', $timestamp, $sequence, $secret);

        $this->assertNotEquals($sig1, $sig2);
    }

    public function test_sign_request_different_timestamp_produces_different_signature() {
        $body = 'test-body';
        $sequence = 1;
        $secret = 'secret';

        $sig1 = HMAC::sign_request($body, '1686000000', $sequence, $secret);
        $sig2 = HMAC::sign_request($body, '1686000001', $sequence, $secret);

        $this->assertNotEquals($sig1, $sig2);
    }

    public function test_sign_request_different_sequence_produces_different_signature() {
        $body = 'test-body';
        $timestamp = '1686000000';
        $secret = 'secret';

        $sig1 = HMAC::sign_request($body, $timestamp, 1, $secret);
        $sig2 = HMAC::sign_request($body, $timestamp, 2, $secret);

        $this->assertNotEquals($sig1, $sig2);
    }

    public function test_get_headers_returns_required_fields() {
        $signature = 'abc123def456';
        $timestamp = '1686000000';
        $sequence = 1;
        $store_id = 'store-123';
        $connector_id = 'conn-abc';

        $headers = HMAC::get_headers($store_id, $connector_id, $signature, $timestamp, $sequence);

        $this->assertArrayHasKey('X-Sarvex-Credential', $headers);
        $this->assertArrayHasKey('X-Sarvex-Timestamp', $headers);
        $this->assertArrayHasKey('X-Sarvex-Sequence', $headers);
        $this->assertArrayHasKey('X-Sarvex-Signature', $headers);
    }

    public function test_get_headers_formats_credential_correctly() {
        $store_id = 'store-xyz';
        $connector_id = 'conn-123';
        $signature = 'sig';
        $timestamp = '1000';
        $sequence = 5;

        $headers = HMAC::get_headers($store_id, $connector_id, $signature, $timestamp, $sequence);

        $this->assertEquals('store-xyz:conn-123', $headers['X-Sarvex-Credential']);
        $this->assertEquals('1000', $headers['X-Sarvex-Timestamp']);
        $this->assertEquals(5, $headers['X-Sarvex-Sequence']);
        $this->assertEquals('sig', $headers['X-Sarvex-Signature']);
    }

    public function test_empty_body_produces_valid_signature() {
        $signature = HMAC::sign_request('', '1000', 0, 'secret');

        $this->assertIsString($signature);
        $this->assertMatchesRegularExpression('/^[0-9a-f]{64}$/', $signature);
    }
}
