<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Analytics surface — single page-head + tab bar over the four analytics
// functions that used to be standalone nav items (Checkouts, Lost Revenue,
// Channel Dashboard, Agent Behavior Insights). Tab state round-trips
// through the URL (?tab=) so a link into a specific tab is shareable,
// matching StoreSurface's convention for its own tabs.

import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import CheckoutsPanel from '@/components/analytics/CheckoutsPanel.vue'
import LostRevenuePanel from '@/components/analytics/LostRevenuePanel.vue'
import ChannelPanel from '@/components/analytics/ChannelPanel.vue'
import AgentInsightsPanel from '@/components/analytics/AgentInsightsPanel.vue'

type Tab = 'checkouts' | 'lost-revenue' | 'channel' | 'insights'
const TABS: Tab[] = ['checkouts', 'lost-revenue', 'channel', 'insights']

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const tab = computed<Tab>(() => {
  const q = route.query.tab
  return typeof q === 'string' && (TABS as string[]).includes(q) ? (q as Tab) : 'checkouts'
})

function setTab(next: Tab) {
  router.replace({ query: { ...route.query, tab: next } })
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">{{ t('analytics.surface.eyebrow') }}</div>
      <h1 class="page-title display">{{ t('analytics.surface.title') }}</h1>
      <p class="page-sub">{{ t('analytics.surface.sub') }}</p>
    </div>

    <nav class="tabs">
      <button :class="{ active: tab === 'checkouts' }" @click="setTab('checkouts')">{{ t('analytics.surface.tabs.checkouts') }}</button>
      <button :class="{ active: tab === 'lost-revenue' }" @click="setTab('lost-revenue')">{{ t('analytics.surface.tabs.lostRevenue') }}</button>
      <button :class="{ active: tab === 'channel' }" @click="setTab('channel')">{{ t('analytics.surface.tabs.channel') }}</button>
      <button :class="{ active: tab === 'insights' }" @click="setTab('insights')">{{ t('analytics.surface.tabs.insights') }}</button>
    </nav>

    <CheckoutsPanel v-if="tab === 'checkouts'" />
    <LostRevenuePanel v-else-if="tab === 'lost-revenue'" />
    <ChannelPanel v-else-if="tab === 'channel'" />
    <AgentInsightsPanel v-else-if="tab === 'insights'" />
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 68ch; }

.tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 26px;
}
.tabs button {
  padding: 11px 4px;
  margin-right: 18px;
  color: var(--muted);
  font-weight: 500;
  font-size: 15px;
  position: relative;
  background: none;
  border: 0;
  cursor: pointer;
}
.tabs button:hover { color: var(--ink); }
.tabs button.active { color: var(--ink); }
.tabs button.active::after {
  content: '';
  position: absolute;
  left: 0; right: 0; bottom: -1px;
  height: 2px;
  background: var(--accent);
  border-radius: 2px;
}
</style>
