<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The Sarvex mark: a lit diamond — a signal held inside a frame. Matches the
// marketing site's SarvexMark.vue exactly (same shape/gradient/glow), so the
// wordmark lockup is identical between sarvex.example.com and the console.
withDefaults(defineProps<{ size?: number }>(), { size: 26 })
</script>

<template>
  <span class="sarvex-mark" :style="{ '--s': `${size}px` }" aria-hidden="true">
    <span class="facet" />
  </span>
</template>

<style scoped>
.sarvex-mark {
  position: relative;
  display: inline-grid;
  place-items: center;
  width: var(--s);
  height: var(--s);
  border-radius: calc(var(--s) * 0.3);
  background: linear-gradient(160deg, var(--raise), var(--surface2));
  border: 1px solid var(--line2);
  flex-shrink: 0;
}
.facet {
  width: calc(var(--s) * 0.34);
  height: calc(var(--s) * 0.34);
  border-radius: calc(var(--s) * 0.08);
  background: var(--accent);
  transform: rotate(45deg);
  box-shadow: 0 0 calc(var(--s) * 0.4) var(--accent-soft);
}
</style>
