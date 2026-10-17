<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class ProfileProxy {
    private $logger;
    private $profile_cache_key = 'sarvex_ucp_profile_cache';
    private $profile_cache_ttl = 3600; // 1 hour
    private $profile_sequence_key = 'sarvex_ucp_profile_sequence';

    public function __construct(Logger $logger) {
        $this->logger = $logger;
    }

    public function handle_request($wp) {
        $request_uri = isset($_SERVER['REQUEST_URI']) ? $_SERVER['REQUEST_URI'] : '';

        // Handle /.well-known/ucp only
        // (UCP API endpoints point directly at Sarvex, not through this proxy)
        if (strpos($request_uri, '/.well-known/ucp') === 0) {
            $this->serve_profile();
            exit;
        }
    }

    private function serve_profile() {
        header('Content-Type: application/json');
        header('Cache-Control: public, max-age=3600');

        // Try to fetch fresh profile from Sarvex
        $profile = $this->fetch_profile_from_sarvex();

        if ($profile === null) {
            // Fall back to cached profile
            $profile = get_transient($this->profile_cache_key);
            if ($profile === false) {
                $this->logger->error('Profile unavailable: no cache and fetch failed');
                http_response_code(503);
                echo json_encode(array('error' => 'Profile service unavailable'));
                return;
            }
            $this->logger->warn('Serving cached profile due to Sarvex unreachable');
        }

        echo $profile;
        http_response_code(200);
    }

    private function fetch_profile_from_sarvex() {
        $endpoint_url = get_option('sarvex_ucp_endpoint_url');
        $store_id = get_option('sarvex_ucp_store_id');
        $connector_id = get_option('sarvex_ucp_connector_id');
        $hmac_secret = get_option('sarvex_ucp_hmac_secret');

        if (empty($endpoint_url) || empty($store_id) || empty($connector_id) || empty($hmac_secret)) {
            return null;
        }

        // Get next sequence number (separate counter for profile fetches)
        $sequence = (int)get_transient($this->profile_sequence_key) + 1;
        set_transient($this->profile_sequence_key, $sequence, 86400 * 365);

        $timestamp = time();
        $body = '';
        $signature = HMAC::sign_request($body, $timestamp, $sequence, $hmac_secret);

        $headers = array_merge(
            array('Content-Type' => 'application/json'),
            HMAC::get_headers($store_id, $connector_id, $signature, $timestamp, $sequence)
        );

        $response = wp_remote_get($endpoint_url . '/connector/profile', array(
            'headers' => $headers,
            'timeout' => 5,
        ));

        if (is_wp_error($response)) {
            $this->logger->warn('Failed to fetch profile from Sarvex', array(
                'error' => $response->get_error_message(),
            ));
            return null;
        }

        $body = wp_remote_retrieve_body($response);
        $status_code = wp_remote_retrieve_response_code($response);

        if ($status_code !== 200) {
            $this->logger->warn('Sarvex returned non-200 for profile', array(
                'status' => $status_code,
            ));
            return null;
        }

        // Cache the profile
        set_transient($this->profile_cache_key, $body, $this->profile_cache_ttl);

        return $body;
    }

}
