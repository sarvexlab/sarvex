<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Primary action button with a built-in loading state: swaps its leading
// icon for an animated dots indicator while `loading` is true and disables
// itself. Trailing content (e.g. an arrow icon) goes in the #after slot so
// this stays usable for buttons with a leading icon, a trailing icon, or
// neither.

import Icon, { type IconName } from '@/components/ui/Icon.vue'

withDefaults(defineProps<{ loading?: boolean; disabled?: boolean; icon?: IconName }>(), {
  loading: false,
  disabled: false,
})
</script>

<template>
  <button type="button" class="btn btn-primary" :disabled="disabled || loading">
    <span v-if="loading" class="dots"><i /><i /><i /></span>
    <Icon v-else-if="icon" :name="icon" />
    <slot />
    <slot name="after" />
  </button>
</template>

<style scoped>
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: var(--radius-sm);
  padding: 10px 16px;
  font-weight: 600;
  font-size: var(--fs-small);
  border: 0;
  cursor: pointer;
  white-space: nowrap;
}
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-deep); }
.btn-primary:disabled { opacity: 0.45; cursor: default; }

.dots {
  display: inline-flex;
  gap: 3px;
  align-items: center;
}
.dots i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #fff;
  display: block;
}
@media (prefers-reduced-motion: no-preference) {
  .dots i { animation: pd 1.2s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: 0.15s; }
  .dots i:nth-child(3) { animation-delay: 0.3s; }
  @keyframes pd { 0%, 100% { opacity: 0.35; } 50% { opacity: 1; } }
}
</style>
