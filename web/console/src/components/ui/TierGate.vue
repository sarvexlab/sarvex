<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Visible-but-locked gate for a billing-tier-restricted surface. The nav
// item that links here always renders (routing never hides the page); this
// component decides, once billing has loaded, whether to show the real
// content (default slot) or a locked panel that explains the value and
// offers an upgrade path — never a blank page or a silent redirect.
//
// `unlocked` is a plain boolean so callers derive it however their gate
// works (today: billing.plan is 'concierge' or 'scale' — see
// lib/ledger.ts's isAnalyticsTier, sarvex-v1-control-plane.yaml's
// requireAnalyticsTier). While `loading` is true neither state renders, to
// avoid flashing "locked" before billing has actually loaded.

import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'

withDefaults(
  defineProps<{
    unlocked: boolean
    loading?: boolean
    lockedTitle: string
    lockedDescription: string
    upgradeCta: string
  }>(),
  { loading: false },
)
</script>

<template>
  <div v-if="loading" class="tier-gate-skel">
    <Skeleton height="220px" radius="var(--radius)" />
  </div>
  <slot v-else-if="unlocked" />
  <div v-else class="locked card">
    <div class="locked-icon"><Icon name="lock" /></div>
    <h2 class="locked-title display">{{ lockedTitle }}</h2>
    <p class="locked-desc">{{ lockedDescription }}</p>
    <router-link to="/settings" class="locked-cta">
      {{ upgradeCta }}
      <Icon name="arrowR" />
    </router-link>
  </div>
</template>

<style scoped>
.tier-gate-skel { padding: 10px 0; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.locked {
  padding: 56px 32px;
  text-align: center;
  max-width: 560px;
  margin: 0 auto;
}
.locked-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent-deep);
  margin-bottom: 18px;
}
.locked-icon :deep(svg) { width: 20px; height: 20px; }
.locked-title { font-size: 22px; margin-bottom: 10px; }
.locked-desc { color: var(--muted); font-size: 14.5px; line-height: 1.6; margin-bottom: 22px; }
.locked-cta {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  background: var(--accent);
  color: #fff;
  font-weight: 600;
  font-size: 13.5px;
  padding: 10px 18px;
  border-radius: var(--radius-sm);
}
.locked-cta:hover { background: var(--accent-deep); }
.locked-cta :deep(svg) { width: 14px; height: 14px; }
</style>
