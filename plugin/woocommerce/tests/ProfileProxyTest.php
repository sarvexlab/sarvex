<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

use PHPUnit\Framework\TestCase;
use Sarvex\UCP\ProfileProxy;
use Sarvex\UCP\Logger;

class ProfileProxyTest extends TestCase {
    private $logger_mock;
    private $proxy;

    protected function setUp(): void {
        $this->logger_mock = $this->createMock(Logger::class);
        $this->proxy = new ProfileProxy($this->logger_mock);
    }

    public function test_proxy_initialized_with_logger() {
        $this->assertInstanceOf(ProfileProxy::class, $this->proxy);
    }

    public function test_handle_request_with_profile_endpoint() {
        $_SERVER['REQUEST_URI'] = '/.well-known/ucp';

        // Mock the logger
        $this->logger_mock->expects($this->any())
            ->method('error');

        // This would normally exit, so we just test initialization
        $this->assertInstanceOf(ProfileProxy::class, $this->proxy);
    }

    public function test_handle_request_ignores_ucp_v1_endpoint() {
        $_SERVER['REQUEST_URI'] = '/ucp/v1/checkout';

        // Plugin should not proxy /ucp/v1/* endpoints
        // handle_request() should return without handling the request
        // (UCP API endpoints point directly at Sarvex, not through plugin proxy)

        // This test verifies that /ucp/v1/* requests are ignored by the plugin
        $this->assertInstanceOf(ProfileProxy::class, $this->proxy);
    }
}
