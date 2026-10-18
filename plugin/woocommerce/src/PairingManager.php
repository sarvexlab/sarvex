<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class PairingManager {
    private $logger;
    private $sequence_key = 'sarvex_ucp_pairing_sequence';

    public function __construct(Logger $logger) {
        $this->logger = $logger;
    }

    /**
     * Complete the pairing flow: exchange pairing code + connector_id from Sarvex console.
     * Called by admin after merchant receives pairing code and connector_id from console.
     *
     * Sends an HMAC-signed request to the backend using the pairing_code as the secret.
     * The pairing_code is then stored as the HMAC secret for all future authenticated requests.
     *
     * @param string $endpoint_url The Sarvex API endpoint
     * @param string $store_id The store_id (required for X-Sarvex-Credential header)
     * @param string $connector_id The connector_id from Sarvex console (returned by begin_pairing)
     * @param string $pairing_code The pairing code from Sarvex console
     * @return bool True if pairing succeeded
     */
    public function complete_pairing($endpoint_url, $store_id, $connector_id, $pairing_code) {
        if (empty($endpoint_url) || empty($store_id) || empty($connector_id) || empty($pairing_code)) {
            $this->logger->warn('Pairing attempt with missing parameters', array(
                'has_endpoint' => !empty($endpoint_url),
                'has_store_id' => !empty($store_id),
                'has_connector_id' => !empty($connector_id),
                'has_pairing_code' => !empty($pairing_code),
            ));
            return false;
        }

        // Prepare the request body
        $body = json_encode(array(
            'connector_id' => $connector_id,
            'pairing_code' => $pairing_code,
        ));

        // Initialize sequence for this pairing request (first authenticated request)
        $sequence = 1;
        $timestamp = gmdate('c'); // RFC3339 UTC timestamp

        // HMAC-sign the request using pairing_code as the secret
        $signature = HMAC::sign_request($body, $timestamp, $sequence, $pairing_code);

        // Build HMAC-authenticated headers
        $headers = array_merge(
            array('Content-Type' => 'application/json'),
            HMAC::get_headers($store_id, $connector_id, $signature, $timestamp, $sequence)
        );

        // POST to Sarvex with HMAC authentication
        $response = wp_remote_post($endpoint_url . '/v1/connectors/pairing/complete', array(
            'method' => 'POST',
            'headers' => $headers,
            'body' => $body,
            'timeout' => 5,
        ));

        if (is_wp_error($response)) {
            $this->logger->error('Pairing request failed', array(
                'error' => $response->get_error_message(),
                'connector_id' => $connector_id,
            ));
            return false;
        }

        $status_code = wp_remote_retrieve_response_code($response);
        if ($status_code !== 200 && $status_code !== 201) {
            $this->logger->error('Pairing failed', array(
                'status' => $status_code,
                'body' => wp_remote_retrieve_body($response),
                'connector_id' => $connector_id,
            ));
            return false;
        }

        $response_body = wp_remote_retrieve_body($response);
        $data = json_decode($response_body, true);

        if (empty($data['connector_id']) || empty($data['store_id'])) {
            $this->logger->error('Pairing response missing required fields', array(
                'body' => $response_body,
                'connector_id' => $connector_id,
            ));
            return false;
        }

        // Store pairing credentials
        update_option('sarvex_ucp_connector_id', $data['connector_id']);
        update_option('sarvex_ucp_store_id', $data['store_id']);
        update_option('sarvex_ucp_endpoint_url', $endpoint_url);

        // Store the pairing_code as the HMAC secret for all future authenticated requests
        update_option('sarvex_ucp_hmac_secret', $pairing_code);

        // Initialize sequence counter for future authenticated requests
        set_transient($this->get_sequence_key(), $sequence, 86400 * 365); // Store for a year

        $this->logger->info('Pairing completed successfully (HMAC-signed)', array(
            'connector_id' => $data['connector_id'],
            'store_id' => $data['store_id'],
        ));

        return true;
    }

    /**
     * Get the sequence key for this site/store combination.
     * This ensures sequence counters are isolated per site.
     */
    private function get_sequence_key() {
        $store_id = get_option('sarvex_ucp_store_id', '');
        return 'sarvex_ucp_signal_sequence' . ($store_id ? '_' . $store_id : '');
    }

    /**
     * Begin pairing flow: request pairing code from Sarvex console for a store.
     * This is called to initiate pairing via the console UI.
     *
     * @param string $endpoint_url The Sarvex API endpoint
     * @param string $store_id The store_id to pair
     * @param string $bearer_token The bearer token for authentication
     * @return array|false Array with pairing_code and store_id, or false on failure
     */
    public function begin_pairing($endpoint_url, $store_id, $bearer_token) {
        if (empty($endpoint_url) || empty($store_id) || empty($bearer_token)) {
            $this->logger->warn('Begin pairing attempt with missing parameters');
            return false;
        }

        // POST to Sarvex to begin pairing
        $response = wp_remote_post($endpoint_url . '/v1/stores/' . urlencode($store_id) . '/connectors/pairing', array(
            'method' => 'POST',
            'headers' => array(
                'Content-Type' => 'application/json',
                'Authorization' => 'Bearer ' . $bearer_token,
            ),
            'body' => json_encode(array(
                'platform' => 'woocommerce',
            )),
            'timeout' => 5,
        ));

        if (is_wp_error($response)) {
            $this->logger->error('Begin pairing request failed', array(
                'error' => $response->get_error_message(),
            ));
            return false;
        }

        $status_code = wp_remote_retrieve_response_code($response);
        if ($status_code !== 201) {
            $this->logger->error('Begin pairing failed', array(
                'status' => $status_code,
                'body' => wp_remote_retrieve_body($response),
            ));
            return false;
        }

        $body = wp_remote_retrieve_body($response);
        $data = json_decode($body, true);

        if (empty($data['pairing_code']) || empty($data['store_id']) || empty($data['connector'])) {
            $this->logger->error('Begin pairing response missing required fields', array(
                'body' => $body,
            ));
            return false;
        }

        // Extract connector_id from the connector object
        $connector_id = isset($data['connector']['id']) ? $data['connector']['id'] : '';
        if (empty($connector_id)) {
            $this->logger->error('Begin pairing response missing connector.id', array(
                'body' => $body,
            ));
            return false;
        }

        return array(
            'connector_id' => $connector_id,
            'pairing_code' => $data['pairing_code'],
            'store_id' => $data['store_id'],
        );
    }

    /**
     * Check if the plugin is paired (has store_id, connector_id, and hmac_secret).
     */
    public function is_paired() {
        $store_id = get_option('sarvex_ucp_store_id');
        $connector_id = get_option('sarvex_ucp_connector_id');
        $hmac_secret = get_option('sarvex_ucp_hmac_secret');
        return !empty($store_id) && !empty($connector_id) && !empty($hmac_secret);
    }
}
