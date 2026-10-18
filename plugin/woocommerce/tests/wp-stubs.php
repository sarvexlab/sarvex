<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

/**
 * Minimal in-memory stand-ins for the WordPress functions the plugin calls,
 * so the unit tests run without a WordPress install. Tests that need to shape
 * an HTTP response set $GLOBALS['sarvex_test_http_response'].
 */

$GLOBALS['sarvex_test_options'] = array();
$GLOBALS['sarvex_test_transients'] = array();
$GLOBALS['sarvex_test_http_response'] = null;

if (!function_exists('get_option')) {
    function get_option($name, $default = false) {
        return array_key_exists($name, $GLOBALS['sarvex_test_options'])
            ? $GLOBALS['sarvex_test_options'][$name]
            : $default;
    }
}
if (!function_exists('update_option')) {
    function update_option($name, $value) {
        $GLOBALS['sarvex_test_options'][$name] = $value;
        return true;
    }
}
if (!function_exists('delete_option')) {
    function delete_option($name) {
        unset($GLOBALS['sarvex_test_options'][$name]);
        return true;
    }
}
if (!function_exists('get_transient')) {
    function get_transient($name) {
        return array_key_exists($name, $GLOBALS['sarvex_test_transients'])
            ? $GLOBALS['sarvex_test_transients'][$name]
            : false;
    }
}
if (!function_exists('set_transient')) {
    function set_transient($name, $value, $expiration = 0) {
        $GLOBALS['sarvex_test_transients'][$name] = $value;
        return true;
    }
}
if (!function_exists('is_wp_error')) {
    function is_wp_error($thing) {
        return $thing instanceof WP_Error;
    }
}
if (!class_exists('WP_Error')) {
    class WP_Error {
        private $message;
        public function __construct($code = '', $message = '') {
            $this->message = $message;
        }
        public function get_error_message() {
            return $this->message;
        }
    }
}
if (!function_exists('wp_remote_post')) {
    function wp_remote_post($url, $args = array()) {
        return $GLOBALS['sarvex_test_http_response']
            ?? new WP_Error('http_request_failed', 'no response configured');
    }
}
if (!function_exists('wp_remote_get')) {
    function wp_remote_get($url, $args = array()) {
        return $GLOBALS['sarvex_test_http_response']
            ?? new WP_Error('http_request_failed', 'no response configured');
    }
}
if (!function_exists('wp_remote_retrieve_response_code')) {
    function wp_remote_retrieve_response_code($response) {
        return $response['response']['code'] ?? 0;
    }
}
if (!function_exists('wp_remote_retrieve_body')) {
    function wp_remote_retrieve_body($response) {
        return $response['body'] ?? '';
    }
}
if (!function_exists('esc_attr')) {
    function esc_attr($text) {
        return htmlspecialchars((string) $text, ENT_QUOTES, 'UTF-8');
    }
}
