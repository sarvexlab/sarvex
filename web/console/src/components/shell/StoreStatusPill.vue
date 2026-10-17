<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Global store-status pill (brief §Phase 2 top bar): Not connected → Setting
// up → Ready to go live → Live. Derived entirely from state the shell
// already loads (connectors + activation) — no new endpoint.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useActivationStore } from '@/stores/activation'
import { needsConnect } from '@/router/needsConnect'
import StatusChip from '@/components/ui/StatusChip.vue'
import type { Status } from '@/components/ui/StatusChip.vue'

const stores = useStoresStore()
const connectors = useConnectorsStore()
const activation = useActivationStore()
const { t } = useI18n()

const state = computed<{ status: Status; label: string } | null>(() => {
  if (!stores.currentStore) return null
  if (needsConnect(stores, connectors)) {
    return { status: 'blocker', label: t('shell.storeStatus.notConnected') }
  }
  const ucpState = activation.activation?.ucp_state
  if (!ucpState) return null
  if (ucpState === 'active') return { status: 'live', label: t('shell.storeStatus.live') }
  if (ucpState === 'suspended') return { status: 'blocker', label: t('shell.storeStatus.needsAttention') }
  if (ucpState === 'verified') return { status: 'attention', label: t('shell.storeStatus.readyToGoLive') }
  return { status: 'attention', label: t('shell.storeStatus.settingUp') }
})
</script>

<template>
  <StatusChip v-if="state" :status="state.status" :label="state.label" />
</template>
