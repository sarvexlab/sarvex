<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class SignalSender {
    private $logger;
    private $sequence_key = 'sarvex_ucp_signal_sequence';

    public function __construct(Logger $logger) {
        $this->logger = $logger;
    }

    public function on_product_save($product_id, $product) {
        if (wp_is_post_revision($product_id) || wp_is_post_autosave($product_id)) {
            return;
        }

        $product_obj = wc_get_product($product_id);
        if (!$product_obj) {
            return;
        }

        $sku = $product_obj->get_sku();
        if (empty($sku)) {
            $this->logger->warn('Product saved without SKU', array('product_id' => $product_id));
            return;
        }

        $this->send_signal(array(
            'sku' => $sku,
            'entity_kind' => 'product',
        ));
    }

    public function on_stock_change($product_id, $stock_quantity) {
        $product = wc_get_product($product_id);
        if (!$product) {
            return;
        }

        $sku = $product->get_sku();
        if (empty($sku)) {
            return;
        }

        $this->send_signal(array(
            'sku' => $sku,
            'entity_kind' => 'inventory',
        ));
    }

    public function on_stock_status_change($product_id, $stock_status, $product) {
        $this->on_stock_change($product_id, null);
    }

    private function send_signal($signal_data) {
        $endpoint_url = get_option('sarvex_ucp_endpoint_url');
        $store_id = get_option('sarvex_ucp_store_id');
        $connector_id = get_option('sarvex_ucp_connector_id');
        $hmac_secret = get_option('sarvex_ucp_hmac_secret');

        if (empty($endpoint_url) || empty($store_id) || empty($connector_id) || empty($hmac_secret)) {
            $this->logger->debug('Skipping signal: pairing not complete', $signal_data);
            return;
        }

        // Get next sequence number
        $sequence = (int)get_transient($this->sequence_key) + 1;
        set_transient($this->sequence_key, $sequence, 86400 * 365); // Store for a year

        $timestamp = time();
        $body = json_encode($signal_data);
        $signature = HMAC::sign_request($body, $timestamp, $sequence, $hmac_secret);

        $args = array(
            'method' => 'POST',
            'headers' => array_merge(
                array(
                    'Content-Type' => 'application/json',
                ),
                HMAC::get_headers($store_id, $connector_id, $signature, $timestamp, $sequence)
            ),
            'body' => $body,
            'timeout' => 5,
        );

        // Send to Sarvex signal endpoint
        $response = wp_remote_post($endpoint_url . '/connector/signals', $args);

        if (is_wp_error($response)) {
            $this->logger->warn('Failed to send signal', array(
                'error' => $response->get_error_message(),
                'signal' => $signal_data,
            ));
        } else {
            $this->logger->debug('Signal sent', array(
                'signal' => $signal_data,
                'sequence' => $sequence,
            ));
        }
    }
}
