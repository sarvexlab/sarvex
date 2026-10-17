<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class Autoloader {
    public static function register($base_dir) {
        // Callers pass the source directory with or without a trailing slash.
        $base_dir = rtrim($base_dir, '/\\') . '/';
        spl_autoload_register(function ($class) use ($base_dir) {
            if (strpos($class, 'Sarvex\\UCP\\') !== 0) {
                return;
            }

            $relative_class = substr($class, strlen('Sarvex\\UCP\\'));
            $file = $base_dir . str_replace('\\', '/', $relative_class) . '.php';

            if (file_exists($file)) {
                require $file;
            }
        });
    }
}
