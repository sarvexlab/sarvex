<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Top bar shown when an operator is "entered as merchant". Carries
// operator identity, the impersonation context, and the audit-log toggle.

import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import { useAuthStore } from '@/stores/auth'
import Icon from '@/components/ui/Icon.vue'

const auth = useAuthStore()
const operator = useOperatorStore()
const router = useRouter()
const { t } = useI18n()

const sameName = computed(
  () => operator.enterAs?.merchantName === operator.enterAs?.storeName,
)

function onExit() {
  operator.exitImpersonation()
  router.push('/board')
}
</script>

<template>
  <div v-if="operator.enterAs" class="opbar">
    <span class="ob-tag">
      <span class="liveping" />
      {{ t('operator.bar.tag') }}
    </span>
    <span class="ob-mid">
      {{ t('operator.bar.viewing') }} <strong>{{ operator.enterAs.storeName ?? operator.enterAs.storeId }}</strong>
      <template v-if="!sameName && operator.enterAs.merchantName">
        · {{ operator.enterAs.merchantName }}
      </template>
      {{ t('operator.bar.exactlyAsSeen') }}
      <strong>{{ auth.principal?.email ?? t('operator.bar.fallbackOperator') }}</strong>.
    </span>
    <button type="button" @click="operator.toggleAudit()">
      <Icon name="note" /> {{ t('operator.bar.auditLog') }}
    </button>
    <button class="ob-exit" type="button" @click="onExit">
      <Icon name="swap" /> {{ t('operator.bar.backToBoard') }}
    </button>
  </div>
</template>

<style scoped>
.opbar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 0 22px;
  height: 46px;
  flex-shrink: 0;
  background: var(--accent-deep);
  color: #fff;
  font-size: 13.5px;
  position: sticky;
  top: 0;
  z-index: 45;
}
.ob-tag {
  font-weight: 600;
  letter-spacing: 0.04em;
  display: flex;
  align-items: center;
  gap: 7px;
}
.liveping {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.6);
  animation: ping 1.8s ease-out infinite;
}
@keyframes ping {
  0%   { box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.5); }
  100% { box-shadow: 0 0 0 7px rgba(255, 255, 255, 0); }
}

.ob-mid { flex: 1; opacity: 0.85; }

button {
  color: #fff;
  font-weight: 600;
  padding: 6px 12px;
  border-radius: 7px;
  font-size: 13px;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.28);
  background: none;
  border: 0;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
button:hover { background: rgba(255, 255, 255, 0.14); }
.ob-exit { box-shadow: none; opacity: 0.85; }
.ob-exit:hover { opacity: 1; }
button :deep(svg) { width: 14px; height: 14px; }
</style>
