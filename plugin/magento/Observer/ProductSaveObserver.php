<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Observer;

use Magento\Framework\Event\Observer as EventObserver;
use Magento\Framework\Event\ObserverInterface;
use Sarvex\Ucp\Model\SignalSender;

/**
 * Signals a product change to Sarvex after the save transaction commits.
 * Bound to catalog_product_save_commit_after (see etc/events.xml) so the
 * signal never races Sarvex's pull against an uncommitted transaction.
 */
class ProductSaveObserver implements ObserverInterface
{
    public function __construct(private readonly SignalSender $signalSender)
    {
    }

    public function execute(EventObserver $observer): void
    {
        $product = $observer->getEvent()->getProduct();
        $sku = $product !== null ? trim((string)$product->getSku()) : '';
        if ($sku === '') {
            return;
        }

        $this->signalSender->sendProductChanged($sku);
    }
}
