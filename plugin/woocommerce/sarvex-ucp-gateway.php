<?php
// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

/**
 * Plugin Name: Sarvex UCP Gateway
 * Plugin URI: https://github.com/sarvexlab/sarvex
 * Description: WooCommerce integration for Google UCP checkout via Sarvex
 * Version: 1.0.0
 * Author: Sarvex Labs
 * License: Apache-2.0
 * License URI: https://www.apache.org/licenses/LICENSE-2.0
 * Text Domain: sarvex-ucp-gateway
 * Domain Path: /languages
 * Requires: 5.0
 * Requires WP: 5.0
 */

defined('ABSPATH') || exit;

// Define plugin constants
define('SARVEX_UCP_PLUGIN_FILE', __FILE__);
define('SARVEX_UCP_PLUGIN_DIR', plugin_dir_path(__FILE__));
define('SARVEX_UCP_PLUGIN_URL', plugin_dir_url(__FILE__));
define('SARVEX_UCP_VERSION', '1.0.0');

// Autoloader
require_once SARVEX_UCP_PLUGIN_DIR . 'src/Autoloader.php';
\Sarvex\UCP\Autoloader::register(SARVEX_UCP_PLUGIN_DIR . 'src');

// Hook plugin activation/deactivation
register_activation_hook(__FILE__, array('\Sarvex\UCP\Plugin', 'activate'));
register_deactivation_hook(__FILE__, array('\Sarvex\UCP\Plugin', 'deactivate'));

// Initialize plugin
add_action('plugins_loaded', array('\Sarvex\UCP\Plugin', 'init'));
