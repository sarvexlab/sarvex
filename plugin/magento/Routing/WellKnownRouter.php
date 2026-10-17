<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

declare(strict_types=1);

namespace Sarvex\Ucp\Routing;

use Magento\Framework\App\Action\Forward;
use Magento\Framework\App\ActionFactory;
use Magento\Framework\App\ActionInterface;
use Magento\Framework\App\RequestInterface;
use Magento\Framework\App\RouterInterface;

/**
 * Matches the literal /.well-known/ucp path so it can be served from the
 * merchant's own domain with a bare 200 (never a redirect) — CLAUDE.md §4's
 * profile-doorway constraint. Magento's standard router only resolves
 * /frontName/controller/action paths, so a dedicated router registered
 * ahead of it (see etc/frontend/di.xml) is the only way to serve a
 * well-known path without depending on webserver-level rewrite
 * configuration the merchant may not control (see README.md for the
 * webserver-rewrite alternative, useful as a fallback/hardening layer).
 *
 * $request is typed to the interface per RouterInterface's contract, but
 * Magento's front controller always passes a concrete
 * \Magento\Framework\App\Request\Http instance, which is what actually
 * implements getPathInfo()/setControllerName()/setDispatched() below.
 *
 * @param \Magento\Framework\App\Request\Http $request
 */
class WellKnownRouter implements RouterInterface
{
    private const WELL_KNOWN_PATH = '.well-known/ucp';

    public function __construct(private readonly ActionFactory $actionFactory)
    {
    }

    public function match(RequestInterface $request): ?ActionInterface
    {
        $pathInfo = trim((string)$request->getPathInfo(), '/');
        if ($pathInfo !== self::WELL_KNOWN_PATH) {
            return null;
        }

        $request->setModuleName('sarvex_ucp')
            ->setControllerName('wellknown')
            ->setActionName('ucp')
            ->setDispatched(false);

        return $this->actionFactory->create(Forward::class);
    }
}
