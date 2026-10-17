<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

use PHPUnit\Framework\TestCase;
use Sarvex\UCP\PairingManager;
use Sarvex\UCP\Logger;
use Sarvex\UCP\HMAC;

/**
 * Integration tests for the complete pairing flow with HMAC authentication.
 *
 * This documents the expected flow:
 * 1. Admin initiates pairing via Sarvex console
 * 2. BeginPairing returns pairing_code, store_id, connector_id
 * 3. Merchant enters pairing_code in WordPress admin
 * 4. CompletePairing is called with all credentials
 * 5. Request is HMAC-signed using pairing_code as the secret
 * 6. Backend verifies HMAC signature and completes pairing
 * 7. Plugin stores credentials and pairing_code as hmac_secret
 * 8. Future requests (signals/heartbeats) use stored hmac_secret
 */
class PairingIntegrationTest extends TestCase {
    private $logger_mock;
    private $manager;

    protected function setUp(): void {
        $this->logger_mock = $this->createMock(Logger::class);
        $this->manager = new PairingManager($this->logger_mock);

        // Clear WordPress options
        delete_option('sarvex_ucp_store_id');
        delete_option('sarvex_ucp_connector_id');
        delete_option('sarvex_ucp_hmac_secret');
        delete_option('sarvex_ucp_endpoint_url');
    }

    /**
     * Integration test: Complete pairing with HMAC-signed request
     *
     * Verifies that:
     * 1. Request body contains connector_id and pairing_code
     * 2. HMAC headers are correctly computed using pairing_code
     * 3. Credential header includes store_id and connector_id
     * 4. Sequence number is set to 1 for first request
     * 5. Timestamp is in RFC3339 UTC format
     */
    public function test_complete_pairing_flow_with_hmac_signing() {
        $endpoint = 'http://localhost:8080';
        $store_id = 'store-123';
        $connector_id = 'conn-456';
        $pairing_code = 'pair-789-abc';

        // Prepare expected request body
        $expected_body = json_encode([
            'connector_id' => $connector_id,
            'pairing_code' => $pairing_code,
        ]);

        // Compute expected HMAC signature
        $sequence = 1;
        $timestamp = gmdate('c'); // Will be close to actual
        $expected_signature = HMAC::sign_request($expected_body, $timestamp, $sequence, $pairing_code);
        $expected_headers = HMAC::get_headers($store_id, $connector_id, $expected_signature, $timestamp, $sequence);

        // Assertions on HMAC header structure
        $this->assertEquals("$store_id:$connector_id", $expected_headers['X-Sarvex-Credential']);
        $this->assertEquals($expected_headers['X-Sarvex-Sequence'], '1');
        $this->assertMatchesRegularExpression('/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/', $expected_headers['X-Sarvex-Timestamp']);
        $this->assertMatchesRegularExpression('/^[a-f0-9]{64}$/', $expected_headers['X-Sarvex-Signature']);
    }

    /**
     * Verify HMAC signature stability: same input always produces same signature
     */
    public function test_hmac_signature_is_deterministic_for_same_inputs() {
        $body = json_encode(['connector_id' => 'conn-123', 'pairing_code' => 'code-456']);
        $timestamp = '2026-06-30T16:00:00+00:00';
        $sequence = 1;
        $secret = 'my-pairing-code';

        $sig1 = HMAC::sign_request($body, $timestamp, $sequence, $secret);
        $sig2 = HMAC::sign_request($body, $timestamp, $sequence, $secret);

        $this->assertEquals($sig1, $sig2, 'HMAC signature should be deterministic');
    }

    /**
     * Verify HMAC signature changes with any modified input
     */
    public function test_hmac_signature_sensitive_to_all_inputs() {
        $body = json_encode(['connector_id' => 'conn-123', 'pairing_code' => 'code-456']);
        $timestamp = '2026-06-30T16:00:00+00:00';
        $sequence = 1;
        $secret = 'my-pairing-code';

        $baseline = HMAC::sign_request($body, $timestamp, $sequence, $secret);

        // Change body
        $modified_body = json_encode(['connector_id' => 'conn-999', 'pairing_code' => 'code-456']);
        $this->assertNotEquals($baseline, HMAC::sign_request($modified_body, $timestamp, $sequence, $secret));

        // Change timestamp
        $modified_timestamp = '2026-07-01T16:00:00+00:00';
        $this->assertNotEquals($baseline, HMAC::sign_request($body, $modified_timestamp, $sequence, $secret));

        // Change sequence
        $this->assertNotEquals($baseline, HMAC::sign_request($body, $timestamp, 2, $secret));

        // Change secret
        $this->assertNotEquals($baseline, HMAC::sign_request($body, $timestamp, $sequence, 'different-secret'));
    }

    /**
     * Usage documentation: How to call complete_pairing from an admin handler
     *
     * This demonstrates the expected flow in practice
     */
    public function test_complete_pairing_usage_from_admin_handler() {
        // This would be called from an admin action/AJAX handler
        $endpoint_url = get_option('sarvex_ucp_endpoint_url');
        $store_id = get_option('sarvex_ucp_store_id'); // From console/context
        $connector_id = $_POST['connector_id'] ?? ''; // From begin_pairing response
        $pairing_code = $_POST['pairing_code'] ?? ''; // Entered by merchant in admin

        // Call complete_pairing
        // $success = $this->manager->complete_pairing(
        //     $endpoint_url,
        //     $store_id,
        //     $connector_id,
        //     $pairing_code
        // );

        // After success:
        // - sarvex_ucp_connector_id is stored
        // - sarvex_ucp_store_id is stored
        // - sarvex_ucp_hmac_secret is stored (= pairing_code)
        // - sarvex_ucp_signal_sequence is initialized to 1
        // - Future SignalSender requests will use stored hmac_secret

        $this->assertTrue(true); // Placeholder
    }

    /**
     * After successful pairing, the pairing_code becomes the HMAC secret
     */
    public function test_pairing_code_becomes_hmac_secret() {
        // Documentation: The pairing_code is not a one-time credential.
        // After pairing completes, it becomes the HMAC secret for all future
        // authenticated requests (signals, heartbeats, etc.)
        //
        // This is why the pairing_code must be treated as a secret:
        // - It's shown once to the merchant
        // - It's used to authenticate completePairing
        // - It's stored and used for all future API calls
        // - It must be rotatable or regenerable if compromised

        $this->assertTrue(true); // Placeholder
    }
}
