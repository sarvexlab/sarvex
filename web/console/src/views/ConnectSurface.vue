<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The one-screen wizard. Per the spec, this is the merchant's irreducible
// hands-on step: install the plugin, receive pairing code, enter code in plugin.
// Everything else is operator-driven or automatic.
//
// Flow:
//   1. Idle: instructions + "Generate pairing code" button.
//   2. On click: POST /stores/:id/connectors/pairing → returns pairing_code + connector_id.
//   3. Show pairing code: user enters in plugin, plugin calls completePairing.
//   4. Poll: GET /connectors/{id} every 700ms until status flips off pairing.
//   5. Paired: success card → onDone routes to /path.

import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useActivationStore } from '@/stores/activation'
import ConnectIdleStep from '@/components/connect/ConnectIdleStep.vue'
import ConnectPairingStep from '@/components/connect/ConnectPairingStep.vue'
import ConnectPairedStep from '@/components/connect/ConnectPairedStep.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import { platformLabelKey } from '@/platforms'

type Phase = 'idle' | 'generating' | 'awaiting-plugin' | 'polling' | 'paired' | 'error'

const pairingError = ref<string | null>(null)

const router = useRouter()
const { t } = useI18n()
const stores = useStoresStore()
const connectors = useConnectorsStore()
const activation = useActivationStore()

const pairingCode = ref('')
const connectorId = ref('')
const phase = ref<Phase>('idle')
let pollTimer: number | null = null

const store = computed(() => stores.currentStore)
const platformLabel = computed(() => t(platformLabelKey(store.value?.platform)))

watch(
  () => store.value?.store_id,
  () => {
    pairingCode.value = ''
    connectorId.value = ''
    phase.value = 'idle'
    pairingError.value = null
    stopPolling()
  },
)

function stopPolling() {
  if (pollTimer !== null) {
    window.clearTimeout(pollTimer)
    pollTimer = null
  }
}

async function pollPaired(connId: string) {
  stopPolling()
  pollTimer = window.setTimeout(async () => {
    try {
      const c = await connectors.refreshOne(connId)
      if (c.status && c.status !== 'pairing') {
        await stores.load()
        phase.value = 'paired'
        // "First check runs automatically once paired" — fire the real
        // conformance check now rather than leaving the merchant to find
        // /validation and trigger it by hand. A run this early legitimately
        // reports the remaining setup steps as failures, which is exactly
        // what should populate the Home checklist/blockers.
        void activation.verify()
        return
      }
    } catch {
      /* keep polling — transient errors stay in polling state */
    }
    if (phase.value === 'polling') pollPaired(connId)
  }, 700)
}

async function onGenerateCode() {
  if ((phase.value !== 'idle' && phase.value !== 'error') || !store.value) return
  phase.value = 'generating'
  pairingError.value = null
  try {
    const result = await connectors.beginPairing(store.value.store_id!, store.value.platform ?? 'woocommerce')
    pairingCode.value = result.pairing_code ?? ''
    connectorId.value = result.connector_id ?? ''
    phase.value = 'awaiting-plugin'
  } catch (e) {
    phase.value = 'error'
    pairingError.value = e instanceof Error ? e.message : t('connect.surface.pairingFailed')
  }
}

function onStartPolling() {
  if (connectorId.value) {
    phase.value = 'polling'
    pollPaired(connectorId.value)
  }
}

async function onDone() {
  if (!store.value?.store_id) return
  // Reload all data to ensure guard sees updated connector status
  await Promise.all([stores.load(), connectors.load()])
  // Ensure current store is set before navigating
  stores.select(store.value.store_id)
  await router.push({ name: 'path' })
}

onBeforeUnmount(stopPolling)
</script>

<template>
  <div class="connect-bg">
    <div class="connect-wrap">
      <div v-if="store" class="card connect-card">
        <ErrorBanner
          v-if="phase === 'error'"
          :message="pairingError ?? t('connect.surface.pairingFailed')"
          :retry-label="t('connect.surface.retryPairing')"
          @retry="onGenerateCode"
        />

        <ConnectPairedStep v-if="phase === 'paired'" :store-name="store.name ?? ''" @done="onDone" />

        <ConnectPairingStep
          v-else-if="phase === 'awaiting-plugin' || phase === 'polling'"
          :store-name="store.name ?? ''"
          :platform-label="platformLabel"
          :pairing-code="pairingCode"
          :connector-id="connectorId"
          :polling="phase === 'polling'"
          @start-polling="onStartPolling"
        />

        <ConnectIdleStep
          v-else
          :store-name="store.name ?? ''"
          :domain="store.domain || store.name || ''"
          :platform-label="platformLabel"
          :generating="phase === 'generating'"
          @generate="onGenerateCode"
        />
      </div>

      <p class="footnote">
        {{ t('connect.surface.footnote1') }}<br />
        {{ t('connect.surface.footnote2') }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.connect-bg {
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 20px 110px;
}
.connect-wrap {
  width: min(720px, 100%);
}

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.connect-card { padding: 34px 36px 30px; }

.footnote {
  text-align: center;
  color: var(--faint);
  font-size: 12.5px;
  margin-top: 18px;
  line-height: 1.6;
}
</style>
