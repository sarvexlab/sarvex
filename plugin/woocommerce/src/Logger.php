<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class Logger {
    private $log_file;

    public function __construct() {
        $uploads_dir = wp_upload_dir();
        $this->log_file = $uploads_dir['basedir'] . '/sarvex-ucp.log';
    }

    public function debug($message, $context = array()) {
        $this->log('DEBUG', $message, $context);
    }

    public function info($message, $context = array()) {
        $this->log('INFO', $message, $context);
    }

    public function warn($message, $context = array()) {
        $this->log('WARN', $message, $context);
    }

    public function error($message, $context = array()) {
        $this->log('ERROR', $message, $context);
    }

    private function log($level, $message, $context = array()) {
        $timestamp = date('Y-m-d H:i:s');
        $context_str = !empty($context) ? ' ' . json_encode($context) : '';
        $line = "[$timestamp] [$level] $message$context_str\n";

        error_log($line, 3, $this->log_file);
    }
}
