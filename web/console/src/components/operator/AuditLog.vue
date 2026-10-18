<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Pop-over over the merchant surface during enter-as-merchant. Reads from
// GET /operator/audit-log filtered to the impersonated merchant.

import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import Icon from '@/components/ui/Icon.vue'

const operator = useOperatorStore()
const { t } = useI18n()

onMounted(operator.loadAudit)
</script>

<template>
  <div class="card audit">
    <div class="head">
      <h4>{{ t('operator.auditLog.title') }}</h4>
      <button type="button" class="icon-btn" :aria-label="t('operator.auditLog.close')" @click="operator.toggleAudit()">
        <Icon name="x" />
      </button>
    </div>
    <div class="divider" />
    <div v-if="operator.audit.length === 0" class="empty">
      {{ t('operator.auditLog.empty') }}
    </div>
    <div v-else class="list">
      <div v-for="e in operator.audit" :key="e.entry_id" class="aline">
        <div class="row1">
          <span class="awho">{{ e.operator_user_id }}</span>
          ·
          <span class="aact">{{ e.action }}</span>
        </div>
        <div v-if="e.detail" class="adetail mono">{{ JSON.stringify(e.detail) }}</div>
        <div class="awhen">{{ e.occurred_at }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.audit {
  position: fixed;
  right: 22px;
  top: 96px;
  width: 340px;
  max-height: 70vh;
  overflow-y: auto;
  z-index: 50;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow-lg);
  padding: 6px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px 8px;
}
.head h4 {
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 0;
}
.icon-btn {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--muted);
  background: none;
  border: 0;
  cursor: pointer;
}
.icon-btn:hover { background: var(--surface2); color: var(--ink); }

.divider { height: 1px; background: var(--line); margin: 0 4px 4px; }

.empty {
  padding: 20px 14px;
  color: var(--faint);
  font-size: 13px;
}
.aline {
  padding: 11px 14px;
  border-radius: var(--radius-sm);
  font-size: 13px;
}
.aline:hover { background: var(--surface2); }
.row1 { font-weight: 500; }
.awho { font-weight: 600; }
.aact { color: var(--accent-deep); }
.adetail { font-size: 11.5px; color: var(--muted); margin-top: 3px; }
.awhen { color: var(--faint); font-size: 12px; margin-top: 4px; }
</style>
