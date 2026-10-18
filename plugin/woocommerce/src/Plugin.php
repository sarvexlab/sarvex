<?php
// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

namespace Sarvex\UCP;

class Plugin {
    private static $instance = null;
    private $signal_sender;
    private $pairing_manager;
    private $profile_proxy;
    private $logger;

    public static function init() {
        if (self::$instance === null) {
            self::$instance = new self();
        }
    }

    public static function activate() {
        // Initialize default options
        if (!get_option('sarvex_ucp_endpoint_url')) {
            add_option('sarvex_ucp_endpoint_url', '');
        }
        if (!get_option('sarvex_ucp_store_id')) {
            add_option('sarvex_ucp_store_id', '');
        }
        if (!get_option('sarvex_ucp_connector_id')) {
            add_option('sarvex_ucp_connector_id', '');
        }
        if (!get_option('sarvex_ucp_hmac_secret')) {
            add_option('sarvex_ucp_hmac_secret', '');
        }
    }

    public static function deactivate() {
        // Cleanup if needed
    }

    private function __construct() {
        $this->logger = new Logger();
        $this->signal_sender = new SignalSender($this->logger);
        $this->pairing_manager = new PairingManager($this->logger);
        $this->profile_proxy = new ProfileProxy($this->logger);

        $this->register_hooks();
    }

    private function register_hooks() {
        // Product change signals
        add_action('save_post_product', array($this->signal_sender, 'on_product_save'), 10, 2);
        add_action('woocommerce_product_set_stock', array($this->signal_sender, 'on_stock_change'), 10, 2);
        add_action('woocommerce_product_set_stock_status', array($this->signal_sender, 'on_stock_status_change'), 10, 3);

        // Profile proxy
        add_action('parse_request', array($this->profile_proxy, 'handle_request'), 1);

        // Admin menu
        add_action('admin_menu', array($this, 'register_admin_menu'));
    }

    public function register_admin_menu() {
        add_menu_page(
            'Sarvex UCP',
            'Sarvex UCP',
            'manage_options',
            'sarvex-ucp-settings',
            array($this, 'render_settings_page'),
            'dashicons-cloud'
        );
    }

    public function render_settings_page() {
        if (!current_user_can('manage_options')) {
            wp_die('Unauthorized');
        }

        $endpoint_url = get_option('sarvex_ucp_endpoint_url');
        $store_id = get_option('sarvex_ucp_store_id');
        $connector_id = get_option('sarvex_ucp_connector_id');
        $hmac_secret = get_option('sarvex_ucp_hmac_secret');
        $is_paired = !empty($store_id) && !empty($connector_id) && !empty($hmac_secret);

        ?>
        <div class="wrap">
            <h1>Sarvex UCP Gateway Settings</h1>
            <form method="post" action="options.php">
                <?php settings_fields('sarvex-ucp-settings'); ?>
                <table class="form-table">
                    <tr>
                        <th scope="row"><label for="sarvex_endpoint_url">Sarvex Endpoint URL</label></th>
                        <td>
                            <input type="url" name="sarvex_ucp_endpoint_url" id="sarvex_endpoint_url"
                                   value="<?php echo esc_attr($endpoint_url); ?>" class="regular-text">
                            <p class="description">The Sarvex API endpoint (e.g., https://api.sarvex.local)</p>
                        </td>
                    </tr>
                    <tr>
                        <th scope="row"><label>Pairing Status</label></th>
                        <td>
                            <?php if ($is_paired): ?>
                                <p style="color: green; font-weight: bold;">✓ Paired</p>
                            <?php else: ?>
                                <p style="color: red; font-weight: bold;">✗ Not Paired</p>
                            <?php endif; ?>
                        </td>
                    </tr>
                    <tr>
                        <th scope="row"><label>Store ID</label></th>
                        <td>
                            <input type="text" name="sarvex_ucp_store_id" value="<?php echo esc_attr($store_id); ?>"
                                   class="regular-text" readonly>
                            <p class="description">Set after pairing via Sarvex console</p>
                        </td>
                    </tr>
                    <tr>
                        <th scope="row"><label>Connector ID</label></th>
                        <td>
                            <input type="text" name="sarvex_ucp_connector_id" value="<?php echo esc_attr($connector_id); ?>"
                                   class="regular-text" readonly>
                            <p class="description">Set after pairing via Sarvex console</p>
                        </td>
                    </tr>
                    <tr>
                        <th scope="row"><label>HMAC Secret</label></th>
                        <td>
                            <input type="password" name="sarvex_ucp_hmac_secret" value="<?php echo esc_attr($hmac_secret); ?>"
                                   class="regular-text" readonly>
                            <p class="description">Set after pairing (stored securely)</p>
                        </td>
                    </tr>
                </table>
                <?php submit_button(); ?>
            </form>
        </div>
        <?php
    }
}
