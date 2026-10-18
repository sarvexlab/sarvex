<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Placeholder surface: loads all settings data, drives the skeleton/error
// state, and hosts the two bands as components per the build spec —
// Account (per merchant, shared across all stores) and Store (per active
// store). Both bands open the single SettingsDrawer to the matching panel.
//
// Endpoints:
//   /account, /account/members  → useAccountStore
//   /billing, billing/portal-link → useBillingStore
//   /notifications/preferences  → useNotificationsStore
//   /connectors                  → useConnectorsStore
//   /stores/{id}/payment-config  → usePaymentStore

import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountStore } from '@/stores/account'
import { useBillingStore } from '@/stores/billing'
import { useNotificationsStore } from '@/stores/notifications'
import { useConnectorsStore } from '@/stores/connectors'
import { usePaymentStore } from '@/stores/payment'
import { useStoreProfileStore } from '@/stores/storeProfile'
import { useStoresStore } from '@/stores/stores'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import Icon from '@/components/ui/Icon.vue'
import AccountSettingsPanel from '@/components/settings/AccountSettingsPanel.vue'
import StoreSettingsPanel from '@/components/settings/StoreSettingsPanel.vue'
import SettingsDrawer, {
  type SettingsPanel,
} from '@/components/settings/SettingsDrawer.vue'

const { t } = useI18n()
const stores = useStoresStore()
const account = useAccountStore()
const billing = useBillingStore()
const notifications = useNotificationsStore()
const connectors = useConnectorsStore()
const payment = usePaymentStore()
const storeProfile = useStoreProfileStore()

const store = computed(() => stores.currentStore)

async function refresh() {
  await Promise.all([
    account.load(),
    billing.load(),
    notifications.load(),
    connectors.load(),
    store.value?.store_id ? payment.load(store.value.store_id) : Promise.resolve(),
    store.value?.store_id ? storeProfile.load(store.value.store_id) : Promise.resolve(),
  ])
}
onMounted(refresh)
watch(() => stores.currentStoreId, refresh)

const showSkeleton = computed(() => !account.loaded && account.loading)

// ── Drawer + toast ──────────────────────────────────────────────────────
const panel = ref<SettingsPanel | null>(null)
const toast = ref<string | null>(null)
let toastTimer: number | null = null

function openPanel(p: SettingsPanel) {
  panel.value = p
}
function closePanel() {
  panel.value = null
}
function showToast(msg: string) {
  toast.value = msg
  if (toastTimer !== null) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 2600)
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">{{ t('settings.surface.eyebrow') }}</div>
      <h1 class="page-title display">{{ t('settings.surface.title') }}</h1>
      <p class="page-sub">
        {{ t('settings.surface.subPrefix') }} <strong>{{ t('settings.surface.accountWord') }}</strong> {{ t('settings.surface.subMiddle') }}
        <strong>{{ t('settings.surface.storeWord') }}</strong> {{ t('settings.surface.subSuffix') }}
        <span v-if="store">{{ t('settings.surface.storeNameAlone', { storeName: store.name }) }}</span><span v-else>{{ t('settings.surface.activeStoreAlone') }}</span>.
      </p>
    </div>

    <ErrorBanner v-if="account.error" :message="account.error" @retry="refresh" />

    <template v-if="showSkeleton">
      <div class="skel-band">
        <Skeleton width="240px" height="22px" />
        <Skeleton height="120px" radius="11px" />
        <Skeleton height="120px" radius="11px" />
        <Skeleton height="170px" radius="11px" />
      </div>
      <div class="skel-band">
        <Skeleton width="200px" height="22px" />
        <Skeleton height="170px" radius="11px" />
        <Skeleton height="120px" radius="11px" />
      </div>
    </template>

    <template v-if="!showSkeleton">
      <AccountSettingsPanel @open-panel="openPanel" />
      <StoreSettingsPanel @open-panel="openPanel" />
    </template>

    <SettingsDrawer v-if="panel" :key="panel" :panel="panel" @close="closePanel" @toast="showToast" />

    <div v-if="toast" class="toast">
      <Icon name="check" />
      <span>{{ toast }}</span>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.skel-band { display: flex; flex-direction: column; gap: 14px; margin-bottom: 30px; }

.toast {
  position: fixed;
  bottom: 84px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 80;
  background: color-mix(in srgb, var(--ink) 93%, transparent);
  color: #fff;
  font-size: 13.5px;
  font-weight: 500;
  padding: 11px 20px;
  border-radius: 99px;
  box-shadow: 0 14px 44px -12px rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  gap: 9px;
}
.toast :deep(svg) { width: 15px; height: 15px; color: var(--ok); }
</style>
