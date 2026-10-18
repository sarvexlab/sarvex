<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// A stage status tile: a fact about the store, not something to act on.
//
// Deliberately *not* a task card. The Path's card language means "this is
// work — yours, ours, or Google's"; wearing it for a metric would tell the
// merchant that "38 orders this week" is something they need to do
// something about. Same tile treatment the checklist counters use, so it
// reads as a readout on sight.

defineProps<{
  value: string
  label: string
  note?: string
  /** Colours the value for a state worth noticing. */
  tone?: 'ok' | 'warn' | 'muted'
}>()
</script>

<template>
  <div class="stat">
    <div :class="['stat-value', 'display', tone]">{{ value }}</div>
    <div class="stat-label">{{ label }}</div>
    <div v-if="note" class="stat-note">{{ note }}</div>
  </div>
</template>

<style scoped>
.stat {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  padding: 16px 18px;
  box-shadow: var(--shadow);
  min-width: 0;
}
.stat-value {
  font-size: 24px;
  line-height: 1.2;
  overflow-wrap: anywhere;
}
.stat-value.ok { color: var(--ok); }
.stat-value.warn { color: var(--warn); }
.stat-value.muted { color: var(--faint); }

.stat-label {
  font-size: 11.5px;
  color: var(--faint);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin-top: 6px;
}
.stat-note {
  color: var(--muted);
  font-size: 12.5px;
  margin-top: 6px;
}
</style>
