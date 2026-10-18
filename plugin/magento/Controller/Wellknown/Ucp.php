<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Controller\Wellknown;

use Magento\Framework\App\Action\HttpGetActionInterface;
use Magento\Framework\Controller\Result\Raw;
use Magento\Framework\Controller\ResultFactory;
use Magento\Framework\Controller\ResultInterface;
use Psr\Log\LoggerInterface;
use Sarvex\Ucp\Model\ProfileCache;
use Sarvex\Ucp\Model\ProfileClient;

/**
 * Serves /.well-known/ucp from the merchant's own domain — the profile
 * doorway half of sarvex-v1-architecture.md's two-role plugin contract. Fetches the
 * profile live from Sarvex on every request, falling back to the last-
 * known-good cached copy if Sarvex is unreachable. Always returns a plain
 * 200 (or 503 if there is truly nothing to serve), never a redirect;
 * matches plugin/woocommerce/src/ProfileProxy.php::serve_profile.
 */
class Ucp implements HttpGetActionInterface
{
    public function __construct(
        private readonly ResultFactory $resultFactory,
        private readonly ProfileClient $profileClient,
        private readonly ProfileCache $profileCache,
        private readonly LoggerInterface $logger
    ) {
    }

    public function execute(): ResultInterface
    {
        /** @var Raw $result */
        $result = $this->resultFactory->create(ResultFactory::TYPE_RAW);
        $result->setHeader('Content-Type', 'application/json');
        $result->setHeader('Cache-Control', 'public, max-age=3600');

        $profile = $this->profileClient->fetch();

        if ($profile !== null) {
            $this->profileCache->save($profile);
            $result->setHttpResponseCode(200);
            $result->setContents($profile);
            return $result;
        }

        $cached = $this->profileCache->load();
        if ($cached === null) {
            $this->logger->error('sarvex_ucp: profile unavailable, no cache and fetch failed');
            $result->setHttpResponseCode(503);
            $result->setContents((string)json_encode(['error' => 'Profile service unavailable']));
            return $result;
        }

        $this->logger->warning('sarvex_ucp: serving cached profile, Sarvex unreachable');
        $result->setHttpResponseCode(200);
        $result->setContents($cached);
        return $result;
    }
}
