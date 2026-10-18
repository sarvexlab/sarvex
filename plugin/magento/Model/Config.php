<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Model;

use Magento\Framework\App\Config\ScopeConfigInterface;
use Magento\Framework\App\Config\Storage\WriterInterface;
use Magento\Store\Model\ScopeInterface;

/**
 * Reads and writes this module's store-scoped configuration
 * (sarvex_ucp/general/*), the Magento equivalent of the WooCommerce
 * plugin's get_option/update_option calls against wp_options
 * (plugin/woocommerce/src/Plugin.php, PairingManager.php).
 */
class Config
{
    private const XML_PATH_ENDPOINT_URL = 'sarvex_ucp/general/endpoint_url';
    private const XML_PATH_STORE_ID = 'sarvex_ucp/general/store_id';
    private const XML_PATH_CONNECTOR_ID = 'sarvex_ucp/general/connector_id';
    private const XML_PATH_HMAC_SECRET = 'sarvex_ucp/general/hmac_secret';
    private const XML_PATH_SEQUENCE_PREFIX = 'sarvex_ucp/general/';

    public function __construct(
        private readonly ScopeConfigInterface $scopeConfig,
        private readonly WriterInterface $configWriter
    ) {
    }

    public function getEndpointUrl(): string
    {
        return (string)$this->scopeConfig->getValue(self::XML_PATH_ENDPOINT_URL, ScopeInterface::SCOPE_STORE);
    }

    public function getStoreId(): string
    {
        return (string)$this->scopeConfig->getValue(self::XML_PATH_STORE_ID, ScopeInterface::SCOPE_STORE);
    }

    public function getConnectorId(): string
    {
        return (string)$this->scopeConfig->getValue(self::XML_PATH_CONNECTOR_ID, ScopeInterface::SCOPE_STORE);
    }

    public function getHmacSecret(): string
    {
        return (string)$this->scopeConfig->getValue(self::XML_PATH_HMAC_SECRET, ScopeInterface::SCOPE_STORE);
    }

    public function isPaired(): bool
    {
        return $this->getStoreId() !== '' && $this->getConnectorId() !== '' && $this->getHmacSecret() !== '';
    }

    public function savePairing(string $endpointUrl, string $storeId, string $connectorId, string $hmacSecret): void
    {
        $this->configWriter->save(self::XML_PATH_ENDPOINT_URL, $endpointUrl);
        $this->configWriter->save(self::XML_PATH_STORE_ID, $storeId);
        $this->configWriter->save(self::XML_PATH_CONNECTOR_ID, $connectorId);
        $this->configWriter->save(self::XML_PATH_HMAC_SECRET, $hmacSecret);
    }

    /**
     * Returns the next sequence number for $kind ("signal" or "profile"),
     * persisted in core_config_data so it survives an admin cache flush —
     * unlike Magento's object cache, which sarvex-v1-architecture.md A10.6 (inbound
     * signals de-dup by sequence) cannot tolerate being reset, since a
     * reset counter would make Sarvex treat a genuinely new signal as an
     * already-seen retry.
     */
    public function nextSequence(string $kind): int
    {
        $path = self::XML_PATH_SEQUENCE_PREFIX . $kind . '_sequence';
        $next = (int)$this->scopeConfig->getValue($path, ScopeInterface::SCOPE_STORE) + 1;
        $this->configWriter->save($path, (string)$next);
        return $next;
    }
}
