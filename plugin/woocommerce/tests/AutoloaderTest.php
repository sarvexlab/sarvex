<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

use PHPUnit\Framework\TestCase;

/**
 * The plugin entry point registers the autoloader with the source directory
 * *without* a trailing slash; classes must still resolve.
 */
class AutoloaderTest extends TestCase {
    public function test_resolves_classes_when_base_dir_has_no_trailing_slash() {
        $this->assertTrue(class_exists('\\Sarvex\\UCP\\HMAC'));
    }

    public function test_ignores_classes_outside_the_plugin_namespace() {
        $this->assertFalse(class_exists('\\Other\\Vendor\\Missing', true));
    }
}
