<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The row of status tiles a stage shows above its task feed.
//
// Separate from the feed on purpose: the feed is the task spine (everything
// actionable), and this is the small set of facts the merchant wants at that
// point in the journey that aren't work for anyone.

import type { Component } from 'vue'

defineProps<{
  label?: string
  stats: Component[]
}>()
</script>

<template>
  <section class="stat-strip">
    <div v-if="label" class="section-label">
      {{ label }}
      <span class="ln" />
    </div>
    <div class="stat-grid">
      <component :is="s" v-for="(s, i) in stats" :key="i" />
    </div>
  </section>
</template>

<style scoped>
.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 34px 4px 13px;
}
.section-label .ln { flex: 1; height: 1px; background: var(--line); }

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
</style>
