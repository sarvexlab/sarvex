<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Store band: configures the active store alone. Every row gets the
// design's Edit / Manage / View action that opens the single SettingsDrawer
// to the matching panel (via the open-panel emit).
//
// Endpoints:
//   /connectors                  → useConnectorsStore
//   /stores/{id}/payment-config  → usePaymentStore
//   /stores/{id}/profile         → useStoreProfileStore

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConnectorsStore } from '@/stores/connectors'
import { usePaymentStore } from '@/stores/payment'
import { useStoreProfileStore } from '@/stores/storeProfile'
import { useStoresStore } from '@/stores/stores'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import type { SettingsPanel } from '@/components/settings/SettingsDrawer.vue'
import { platformLabelKey } from '@/platforms'

const emit = defineEmits<{ (e: 'open-panel', panel: SettingsPanel): void }>()

const stores = useStoresStore()
const connectors = useConnectorsStore()
const payment = usePaymentStore()
const storeProfile = useStoreProfileStore()
const { t } = useI18n()

const store = computed(() => stores.currentStore)
const connector = computed(() =>
  store.value?.store_id ? connectors.forStore(store.value.store_id) : null,
)

const handlerSummary = computed(() => {
  const hs = payment.config?.handlers ?? []
  if (hs.length === 0) return t('settings.store.noHandlers')
  return hs
    .map((h) => `${h.handler_id} (${h.config_status ?? 'not_started'})`)
    .join(', ')
})

function platformLabel(p: string | undefined) {
  return t(platformLabelKey(p))
}

function openPanel(p: SettingsPanel) {
  emit('open-panel', p)
}
</script>

<template>
  <section v-if="store" class="band">
    <div class="band-head">
      <span class="band-ic"><Icon name="store" /></span>
      <div>
        <div class="band-title">{{ t('settings.store.title') }}</div>
        <div class="band-sub">{{ t('settings.store.configuresOnly', { storeName: store.name }) }} · {{ store.domain }}</div>
      </div>
    </div>

    <div class="section-label">{{ t('settings.store.sectionStorefront') }}<span class="ln" /></div>
    <div class="card list">
      <div class="row first">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.storeName') }}</div>
          <div class="row-value">{{ store.name }}</div>
        </div>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.platform') }}</div>
          <div class="row-value">{{ platformLabel(store.platform) }}</div>
        </div>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.businessProfile') }}</div>
          <div class="row-value">
            {{ storeProfile.profile?.legal_name || '—' }}
            <template v-if="storeProfile.profile?.fulfillment_regions?.length">
              · {{ t('settings.store.ships', { regions: storeProfile.profile.fulfillment_regions.join(', ') }) }}
            </template>
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('profile')">{{ t('settings.store.edit') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.domainAndProfile') }}</div>
          <div class="row-value mono">{{ store.domain }}/.well-known/ucp</div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('domain')">{{ t('settings.store.view') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.returnPolicy') }}</div>
          <div class="row-value mono break">
            {{ storeProfile.profile?.return_policy_url ?? '—' }}
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('return')">{{ t('settings.store.edit') }}</button>
      </div>
    </div>

    <div class="section-label">{{ t('settings.store.sectionConnectionPayments') }}<span class="ln" /></div>
    <div class="card list">
      <div class="row first">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.connector') }}</div>
          <div class="row-value">
            <template v-if="connector">
              <Chip :kind="connector.status === 'healthy' ? 'ok' : connector.status === 'pairing' ? 'quiet' : 'warn'">
                {{ connector.status }}
              </Chip>
              <span v-if="connector.plugin_version" class="mono"> · v{{ connector.plugin_version }}</span>
            </template>
            <template v-else>{{ t('settings.store.notConnected') }}</template>
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('connector')">{{ t('settings.store.view') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.store.paymentHandlers') }}</div>
          <div class="row-value">{{ handlerSummary }}</div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('handlers')">{{ t('settings.store.manage') }}</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.band { margin-bottom: 30px; }
.band-head { display: flex; align-items: center; gap: 13px; margin: 6px 0 4px; }
.band-ic {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: var(--accent-soft);
  color: var(--accent-deep);
}
.band-ic :deep(svg) { width: 19px; height: 19px; }
.band-title {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 21px;
  letter-spacing: var(--display-tracking);
}
.band-sub { font-size: 13px; color: var(--muted); margin-top: 1px; }

.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 24px 4px 10px;
}
.section-label .ln { flex: 1; height: 1px; background: var(--line); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.list { overflow: hidden; }
.row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 22px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }
.row-main { flex: 1; min-width: 0; }
.row-label { font-weight: 500; }
.row-value {
  color: var(--muted);
  font-size: 13.5px;
  margin-top: 3px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.row-value.mono { font-family: var(--font-mono); }
.row-value.break { word-break: break-all; }
.divider { height: 1px; background: var(--line); border: 0; }

.btn-ghost {
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
  border: 0;
  border-radius: var(--radius-sm);
  padding: 7px 13px;
  font-weight: 600;
  font-size: 12.5px;
  cursor: pointer;
  white-space: nowrap;
}
.btn-ghost:hover { box-shadow: inset 0 0 0 1px var(--faint); }
</style>
