<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class HMAC {
    public static function sign_request($body, $timestamp, $sequence, $hmac_secret) {
        $message = $body . $timestamp . $sequence;
        $signature = hash_hmac('sha256', $message, $hmac_secret, false);
        return $signature;
    }

    public static function get_headers($store_id, $connector_id, $signature, $timestamp, $sequence) {
        return array(
            'X-Sarvex-Credential' => "$store_id:$connector_id",
            'X-Sarvex-Timestamp' => $timestamp,
            'X-Sarvex-Sequence' => $sequence,
            'X-Sarvex-Signature' => $signature,
        );
    }
}
