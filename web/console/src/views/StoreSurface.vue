<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// "What am I paying for", read-only, 3 tabs:
//   Presence — StorePresencePanel
//   Products — StoreProductsPanel (table + ProductDrawer)
//   Orders   — StoreOrdersPanel
//
// This view composes the three panels and owns the tab state, page head,
// and the cross-store refresh on mount / store switch. Each panel reads
// its own Pinia store directly.

import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { usePresenceStore } from '@/stores/presence'
import { useCatalogStore } from '@/stores/catalog'
import { useOrdersStore } from '@/stores/orders'
import StorePresencePanel from '@/components/store/StorePresencePanel.vue'
import StoreProductsPanel from '@/components/store/StoreProductsPanel.vue'
import StoreOrdersPanel from '@/components/store/StoreOrdersPanel.vue'

type Tab = 'presence' | 'products' | 'orders'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const stores = useStoresStore()
const presence = usePresenceStore()
const catalog = useCatalogStore()
const orders = useOrdersStore()

const store = computed(() => stores.currentStore)

// The sidebar's Catalog/Orders entries deep-link here via ?tab= — keep the
// URL as the source of truth so a bookmark/refresh reopens the same tab.
const validTabs: Tab[] = ['presence', 'products', 'orders']
function tabFromRoute(): Tab {
  const q = route.query.tab as string | undefined
  return (validTabs as string[]).includes(q ?? '') ? (q as Tab) : 'presence'
}
const tab = ref<Tab>(tabFromRoute())
watch(() => route.query.tab, () => { tab.value = tabFromRoute() })
function selectTab(next: Tab) {
  tab.value = next
  router.replace({ query: { ...route.query, tab: next } })
}

// A deep-link like /store?product=<sku> implies the Products tab.
watch(
  () => route.query.product as string | undefined,
  (sku) => {
    if (sku) tab.value = 'products'
  },
  { immediate: true },
)

async function refresh() {
  await Promise.all([presence.load(), catalog.load(), orders.load()])
}
onMounted(refresh)
watch(() => stores.currentStoreId, refresh)
</script>

<template>
  <div class="page">
    <div v-if="store" class="page-head">
      <div class="eyebrow">{{ t('store.surface.eyebrow', { storeName: store.name }) }}</div>
      <h1 class="page-title display">{{ t('store.surface.title') }}</h1>
      <p class="page-sub">
        {{ t('store.surface.sub', { storeName: store.name }) }}
      </p>
    </div>

    <nav class="tabs">
      <button :class="{ active: tab === 'presence' }" @click="selectTab('presence')">{{ t('store.surface.tabs.presence') }}</button>
      <button :class="{ active: tab === 'products' }" @click="selectTab('products')">{{ t('store.surface.tabs.products') }}</button>
      <button :class="{ active: tab === 'orders' }" @click="selectTab('orders')">{{ t('store.surface.tabs.orders') }}</button>
    </nav>

    <StorePresencePanel v-if="tab === 'presence'" @retry="refresh" />
    <StoreProductsPanel v-else-if="tab === 'products'" @retry="refresh" />
    <StoreOrdersPanel v-else-if="tab === 'orders'" @retry="orders.load()" />
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 26px;
}
.tabs button {
  padding: 11px 4px;
  margin-right: 18px;
  color: var(--muted);
  font-weight: 500;
  font-size: 15px;
  position: relative;
  background: none;
  border: 0;
  cursor: pointer;
}
.tabs button:hover { color: var(--ink); }
.tabs button.active { color: var(--ink); }
.tabs button.active::after {
  content: '';
  position: absolute;
  left: 0; right: 0; bottom: -1px;
  height: 2px;
  background: var(--accent);
  border-radius: 2px;
}
</style>
