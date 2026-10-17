<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Observer;

use Magento\Catalog\Api\ProductRepositoryInterface;
use Magento\Framework\Event\Observer as EventObserver;
use Magento\Framework\Event\ObserverInterface;
use Sarvex\Ucp\Model\SignalSender;

/**
 * Signals an inventory change to Sarvex after a stock item save commits.
 * Bound to cataloginventory_stock_item_save_commit_after (see
 * etc/events.xml).
 */
class StockItemSaveObserver implements ObserverInterface
{
    public function __construct(
        private readonly SignalSender $signalSender,
        private readonly ProductRepositoryInterface $productRepository
    ) {
    }

    public function execute(EventObserver $observer): void
    {
        $item = $observer->getEvent()->getItem();
        if ($item === null) {
            return;
        }

        $sku = trim((string)$item->getSku());
        if ($sku === '' && $item->getProductId()) {
            $sku = $this->resolveSkuFromProductId((int)$item->getProductId());
        }

        if ($sku === '') {
            return;
        }

        $this->signalSender->sendInventoryChanged($sku);
    }

    private function resolveSkuFromProductId(int $productId): string
    {
        try {
            return trim((string)$this->productRepository->getById($productId)->getSku());
        } catch (\Throwable $e) {
            return '';
        }
    }
}
