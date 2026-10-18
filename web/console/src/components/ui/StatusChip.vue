<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The one status vocabulary used everywhere in the redesigned console.
// Colour is never the only signal — every chip pairs it with an icon and a
// label (contrast/colour-blind safety, and it reads fine in a screen reader
// with the icon's aria-hidden and the label as the accessible text).
import Icon, { type IconName } from './Icon.vue'

export type Status = 'blocker' | 'attention' | 'passing' | 'live'

const props = defineProps<{ status: Status; label: string }>()

const ICONS: Record<Status, IconName> = {
  blocker: 'alert',
  attention: 'flag',
  passing: 'check',
  live: 'pulse',
}
const icon = ICONS[props.status]
</script>

<template>
  <span :class="['status-chip', `is-${status}`]">
    <Icon :name="icon" :size="12" />
    {{ label }}
  </span>
</template>

<style scoped>
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  padding: 3px 10px;
  border-radius: 999px;
  white-space: nowrap;
  line-height: 1.5;
}
.is-blocker   { background: color-mix(in oklab, var(--fail) 14%, var(--surface));   color: var(--fail); }
.is-attention { background: color-mix(in oklab, var(--flag) 14%, var(--surface));   color: var(--flag); }
.is-passing   { background: color-mix(in oklab, var(--pass) 13%, var(--surface));   color: var(--pass); }
.is-live      { background: var(--accent-soft); color: var(--accent-deep); }
</style>
