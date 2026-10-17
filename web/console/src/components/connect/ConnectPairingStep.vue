<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Connect wizard — step 2: show the generated pairing code + connector id,
// then let the merchant confirm they entered it in the plugin (polls
// afterwards to detect pairing completion).

import { useI18n } from 'vue-i18n'
import Mark from '@/components/shell/Mark.vue'
import CopyField from '@/components/ui/CopyField.vue'
import LoadingButton from '@/components/ui/LoadingButton.vue'

defineProps<{
  storeName: string
  platformLabel: string
  pairingCode: string
  connectorId: string
  polling: boolean
}>()

const emit = defineEmits<{ 'start-polling': [] }>()

const { t } = useI18n()
</script>

<template>
  <div class="head">
    <div class="brandline"><Mark /> {{ t('connect.brandline') }}</div>
    <h1 class="display title">{{ t('connect.pairing.title', { storeName }) }}</h1>
    <p class="lede">
      {{ t('connect.pairing.lede', { platformLabel }) }}
    </p>
  </div>

  <div class="code-section">
    <CopyField :label="t('connect.pairing.pairingCodeLabel')" :title="t('connect.pairing.pairingCodeTitle')" :value="pairingCode" />
    <CopyField
      :label="t('connect.pairing.connectorIdLabel')"
      :title="t('connect.pairing.connectorIdTitle')"
      :value="connectorId"
    />
  </div>

  <div class="instructions">
    <p>{{ t('connect.pairing.instructions1', { platformLabel }) }}</p>
    <p>{{ t('connect.pairing.instructions2') }}</p>
  </div>

  <LoadingButton
    class="start-polling-btn"
    icon="zap"
    :loading="polling"
    @click="emit('start-polling')"
  >
    {{ polling ? t('connect.pairing.verifying') : t('connect.pairing.verify') }}
  </LoadingButton>
</template>

<style scoped>
.head { margin-bottom: 22px; }
.brandline {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 13px;
  letter-spacing: -0.01em;
  color: var(--muted);
}
.title { font-size: 28px; margin-top: 6px; }
.lede {
  color: var(--muted);
  margin-top: 10px;
  max-width: 60ch;
}

.code-section {
  background: color-mix(in oklab, var(--accent-soft) 8%, var(--surface));
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 18px;
  margin: 20px 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.instructions {
  color: var(--muted);
  font-size: 14px;
  line-height: 1.6;
  margin: 16px 0;
}
.instructions p { margin: 0 0 8px; }
.instructions p:last-child { margin-bottom: 0; }

.start-polling-btn { margin-top: 12px; }
</style>
