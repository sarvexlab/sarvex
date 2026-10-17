<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Recoverable-error card. Shown above the failed surface area; click Retry
// to re-run the load. The error message comes from the store; this
// component is purely presentational.

import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'

defineProps<{ message: string; retryLabel?: string }>()
defineEmits<{ retry: [] }>()

const { t } = useI18n()
</script>

<template>
  <div class="err" role="alert">
    <div class="row">
      <span class="ic"><Icon name="info" /></span>
      <div class="body">
        <div class="title">{{ t('common.error.title') }}</div>
        <div class="msg">{{ message }}</div>
      </div>
      <button type="button" class="btn" @click="$emit('retry')">
        {{ retryLabel ?? t('common.error.retry') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.err {
  background: color-mix(in oklab, var(--danger) 8%, var(--surface));
  border: 1px solid color-mix(in oklab, var(--danger) 30%, var(--line));
  border-radius: var(--radius);
  padding: 14px 18px;
  margin-bottom: 18px;
}
.row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.ic {
  color: var(--danger);
  display: inline-flex;
  flex-shrink: 0;
  margin-top: 2px;
}
.ic :deep(svg) { width: 18px; height: 18px; }
.body { flex: 1; min-width: 0; }
.title { font-weight: 600; color: var(--ink); }
.msg { color: var(--muted); font-size: 13.5px; margin-top: 3px; }
.btn {
  flex-shrink: 0;
  border: 0;
  border-radius: var(--radius-sm);
  padding: 7px 14px;
  font-weight: 600;
  font-size: 12.5px;
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
  cursor: pointer;
}
.btn:hover { box-shadow: inset 0 0 0 1px var(--faint); }
</style>
