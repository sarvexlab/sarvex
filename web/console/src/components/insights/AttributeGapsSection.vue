<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Attribute gaps (R14) — no backend endpoint exists. Per user direction
// (2026-08-08): keep the section in its required position (R3) but render
// an honest explanatory state instead of data, rather than omitting the
// section or blocking the rest of the page on it. The reason is
// architectural, not "not built yet": sarvex-v1-control-plane.yaml's own
// comment above /insights/capability_gaps says the UCP request types have
// no protocol-level field for an agent to name a requested attribute, so
// there is nothing honest to fetch or display. No store, no loading state,
// no retry — there is nothing to load.

import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'

const { t } = useI18n()
</script>

<template>
  <section class="section">
    <div class="section-head">
      <h2 class="section-title display">{{ t('insights.attributeGaps.title') }}</h2>
      <p class="section-sub">{{ t('insights.attributeGaps.subtitle') }}</p>
    </div>

    <div class="card unavailable-note">
      <div class="ic"><Icon name="info" /></div>
      <div>
        <div class="big">{{ t('insights.attributeGaps.unavailable.title') }}</div>
        <p>{{ t('insights.attributeGaps.unavailable.body') }}</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.section { display: flex; flex-direction: column; gap: 12px; }
.section-title { font-size: 22px; }
.section-sub { color: var(--muted); font-size: 14px; margin-top: 6px; max-width: 68ch; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.unavailable-note {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 22px 24px;
}
.ic { color: var(--faint); flex-shrink: 0; margin-top: 2px; }
.ic :deep(svg) { width: 18px; height: 18px; }
.big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 15px;
  color: var(--ink);
  margin-bottom: 4px;
}
.unavailable-note p { color: var(--muted); font-size: 13.5px; line-height: 1.6; max-width: 62ch; margin: 0; }
</style>
