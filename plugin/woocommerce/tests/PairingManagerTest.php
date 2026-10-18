<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

use PHPUnit\Framework\TestCase;
use Sarvex\UCP\PairingManager;
use Sarvex\UCP\Logger;
use Sarvex\UCP\HMAC;

class PairingManagerTest extends TestCase {
    private $logger_mock;
    private $manager;
    private const TEST_ENDPOINT = 'http://localhost:8080';
    private const TEST_STORE_ID = 'store-abc123';
    private const TEST_CONNECTOR_ID = 'conn-def456';
    private const TEST_PAIRING_CODE = 'pair-ghi789';

    protected function setUp(): void {
        $this->logger_mock = $this->createMock(Logger::class);
        $this->manager = new PairingManager($this->logger_mock);

        // Clear WordPress options between tests
        delete_option('sarvex_ucp_store_id');
        delete_option('sarvex_ucp_connector_id');
        delete_option('sarvex_ucp_hmac_secret');
        delete_option('sarvex_ucp_endpoint_url');
        $GLOBALS['sarvex_test_transients'] = array();
        $GLOBALS['sarvex_test_http_response'] = null;
    }


    private function pair_successfully() {
        $GLOBALS['sarvex_test_http_response'] = array(
            'response' => array('code' => 200),
            'body' => json_encode(array('connector_id' => self::TEST_CONNECTOR_ID, 'store_id' => self::TEST_STORE_ID)),
        );
        return $this->manager->complete_pairing(
            self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE
        );
    }

    // ── Parameter Validation ──────────────────────────────────────────────

    public function test_is_paired_false_when_missing_credentials() {
        $is_paired = $this->manager->is_paired();
        $this->assertFalse($is_paired);
    }

    public function test_complete_pairing_rejects_empty_endpoint() {
        $result = $this->manager->complete_pairing(
            '',
            self::TEST_STORE_ID,
            self::TEST_CONNECTOR_ID,
            self::TEST_PAIRING_CODE
        );
        $this->assertFalse($result);
    }

    public function test_complete_pairing_rejects_empty_store_id() {
        $result = $this->manager->complete_pairing(
            self::TEST_ENDPOINT,
            '',
            self::TEST_CONNECTOR_ID,
            self::TEST_PAIRING_CODE
        );
        $this->assertFalse($result);
    }

    public function test_complete_pairing_rejects_empty_connector_id() {
        $result = $this->manager->complete_pairing(
            self::TEST_ENDPOINT,
            self::TEST_STORE_ID,
            '',
            self::TEST_PAIRING_CODE
        );
        $this->assertFalse($result);
    }

    public function test_complete_pairing_rejects_empty_pairing_code() {
        $result = $this->manager->complete_pairing(
            self::TEST_ENDPOINT,
            self::TEST_STORE_ID,
            self::TEST_CONNECTOR_ID,
            ''
        );
        $this->assertFalse($result);
    }

    public function test_complete_pairing_logs_missing_parameters() {
        $this->logger_mock->expects($this->once())
            ->method('warn')
            ->with(
                'Pairing attempt with missing parameters',
                $this->anything()
            );

        $this->manager->complete_pairing('', self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, '');
    }

    // ── HMAC Request Signing ──────────────────────────────────────────────

    public function test_complete_pairing_signs_request_with_hmac() {
        $this->assertTrue($this->pair_successfully());
        $this->assertTrue($this->manager->is_paired());
    }

    public function test_complete_pairing_request_includes_hmac_headers() {
        // Verify HMAC headers are structured correctly
        $body = json_encode([
            'connector_id' => self::TEST_CONNECTOR_ID,
            'pairing_code' => self::TEST_PAIRING_CODE,
        ]);
        $timestamp = gmdate('c');
        $sequence = 1;

        $signature = HMAC::sign_request($body, $timestamp, $sequence, self::TEST_PAIRING_CODE);
        $headers = HMAC::get_headers(self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, $signature, $timestamp, $sequence);

        // Verify header structure
        $this->assertArrayHasKey('X-Sarvex-Credential', $headers);
        $this->assertArrayHasKey('X-Sarvex-Timestamp', $headers);
        $this->assertArrayHasKey('X-Sarvex-Sequence', $headers);
        $this->assertArrayHasKey('X-Sarvex-Signature', $headers);

        // Verify credential format
        $this->assertEquals(
            self::TEST_STORE_ID . ':' . self::TEST_CONNECTOR_ID,
            $headers['X-Sarvex-Credential']
        );

        // Verify signature is hex (SHA256)
        $this->assertMatchesRegularExpression('/^[a-f0-9]{64}$/', $headers['X-Sarvex-Signature']);
    }

    public function test_hmac_signature_changes_with_different_pairing_code() {
        $body = json_encode(['connector_id' => self::TEST_CONNECTOR_ID, 'pairing_code' => self::TEST_PAIRING_CODE]);
        $timestamp = gmdate('c');
        $sequence = 1;

        $sig1 = HMAC::sign_request($body, $timestamp, $sequence, self::TEST_PAIRING_CODE);
        $sig2 = HMAC::sign_request($body, $timestamp, $sequence, 'different-code');

        $this->assertNotEquals($sig1, $sig2, 'Signature should change with different pairing code');
    }

    public function test_hmac_signature_changes_with_different_sequence() {
        $body = json_encode(['connector_id' => self::TEST_CONNECTOR_ID, 'pairing_code' => self::TEST_PAIRING_CODE]);
        $timestamp = gmdate('c');

        $sig1 = HMAC::sign_request($body, $timestamp, 1, self::TEST_PAIRING_CODE);
        $sig2 = HMAC::sign_request($body, $timestamp, 2, self::TEST_PAIRING_CODE);

        $this->assertNotEquals($sig1, $sig2, 'Signature should change with different sequence');
    }

    // ── Credentials Storage ───────────────────────────────────────────────

    public function test_complete_pairing_stores_connector_credentials() {
        $this->assertTrue($this->pair_successfully());
        $this->assertSame(self::TEST_CONNECTOR_ID, get_option('sarvex_ucp_connector_id'));
        $this->assertSame(self::TEST_STORE_ID, get_option('sarvex_ucp_store_id'));
        $this->assertSame(self::TEST_ENDPOINT, get_option('sarvex_ucp_endpoint_url'));
    }

    public function test_complete_pairing_stores_hmac_secret_from_pairing_code() {
        $this->assertTrue($this->pair_successfully());
        $this->assertSame(self::TEST_PAIRING_CODE, get_option('sarvex_ucp_hmac_secret'));
    }

    public function test_complete_pairing_initializes_sequence_counter() {
        $this->assertTrue($this->pair_successfully());
        $this->assertSame(1, get_transient('sarvex_ucp_signal_sequence_' . self::TEST_STORE_ID));
    }

    // ── Error Handling ────────────────────────────────────────────────────

    public function test_complete_pairing_handles_http_error() {
        $this->logger_mock->expects($this->once())
            ->method('error');

        $GLOBALS['sarvex_test_http_response'] = new WP_Error('http_request_failed', 'connection refused');
        $this->assertFalse($this->manager->complete_pairing(
            self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE
        ));
    }

    public function test_complete_pairing_handles_invalid_response() {
        $this->logger_mock->expects($this->once())
            ->method('error');

        $GLOBALS['sarvex_test_http_response'] = array(
            'response' => array('code' => 200),
            'body' => json_encode(array('unexpected' => true)),
        );
        $this->assertFalse($this->manager->complete_pairing(
            self::TEST_ENDPOINT, self::TEST_STORE_ID, self::TEST_CONNECTOR_ID, self::TEST_PAIRING_CODE
        ));
    }
}
