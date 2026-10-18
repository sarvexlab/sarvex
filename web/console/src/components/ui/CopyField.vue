<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Labelled read-only value with a one-click copy-to-clipboard button. Used
// wherever a generated token (code, id, key) needs to be shown and copied.

import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'

const props = defineProps<{ label: string; value: string; title?: string }>()

const { t } = useI18n()

function copy() {
  navigator.clipboard.writeText(props.value)
}
</script>

<template>
  <div class="code-item">
    <label class="code-label">{{ label }}</label>
    <div class="code-box">
      <code class="mono">{{ value }}</code>
      <button type="button" class="copy-btn" :title="title ?? t('common.copyField.copyTitle', { label })" @click="copy">
        <Icon name="copy" :size="16" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.code-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.code-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.code-box {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  background: var(--surface);
  border: 1px solid var(--line2);
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 14px;
  color: var(--ink);
}
.code-box code { flex: 1; user-select: all; }
.copy-btn {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--muted);
  border-radius: var(--radius-sm);
  transition: color 0.15s, background 0.15s;
}
.copy-btn:hover { color: var(--accent-deep); background: var(--accent-soft); }
</style>
