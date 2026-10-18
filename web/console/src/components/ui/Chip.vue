<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Small status pill. Kinds match the design's chip-* classes: `you` is the
// merchant (accent), `us` is Sarvex (muted), `external` is outlined; plus the
// semantic states ok / warn / danger / quiet.

export type ChipKind = 'you' | 'us' | 'external' | 'ok' | 'warn' | 'danger' | 'quiet'

defineProps<{ kind: ChipKind }>()
</script>

<template>
  <span :class="['chip', `chip-${kind}`]">
    <slot />
  </span>
</template>

<style scoped>
.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  padding: 3px 9px;
  border-radius: 99px;
  white-space: nowrap;
  line-height: 1.4;
}
.chip :deep(svg) { width: 11px; height: 11px; }

.chip-you     { background: var(--accent-soft); color: var(--accent-deep); }
.chip-us      { background: var(--surface2);    color: var(--muted); }
.chip-external  { background: transparent;        color: var(--muted); box-shadow: inset 0 0 0 1px var(--line2); }
.chip-ok      { background: color-mix(in oklab, var(--ok) 12%, var(--surface));     color: var(--ok); }
.chip-warn    { background: color-mix(in oklab, var(--warn) 13%, var(--surface));   color: var(--warn); }
.chip-danger  { background: color-mix(in oklab, var(--danger) 11%, var(--surface)); color: var(--danger); }
.chip-quiet   { background: var(--surface2);    color: var(--faint); }
</style>
