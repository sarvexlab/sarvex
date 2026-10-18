<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Daily trend (R9): initiated vs completed as lines, revenue on a secondary
// axis, surface-filterable. The surface filter itself lives on ChannelPanel
// (R3) — this component only renders whatever `daily` rows it's handed.
//
// GET /channel/daily never aggregates across surfaces (see lib/channel.ts's
// collapseDailyBySurface doc comment) — when the page-level filter is "all
// surfaces" the raw rows carry one entry per surface per date, so this
// collapses them to one point per date before charting.
//
// R9: the X axis renders each UTC-bucketed date in the viewer's local
// timezone (localDateLabel), and a fixed on-chart note discloses that the
// underlying buckets are UTC calendar days — R9 explicitly forbids silently
// re-bucketing, so the note stays visible rather than living in a tooltip.
//
// Currency (R5): each daily row carries its own currency; resolveDailyCurrency
// (lib/channel.ts) checks agreement across the fetched rows the same way the
// backend does for its own rollups. A genuine currency conflict across the
// visible range drops the revenue line + its axis entirely rather than
// plotting dollar amounts under one assumed currency — initiated/completed
// (plain counts) still render.

import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Tooltip,
  Legend,
  type ChartDataset,
  type ChartOptions,
  type TooltipItem,
} from 'chart.js'
import { useThemeStore } from '@/stores/theme'
import { formatMinorUnits } from '@/lib/money'
import { collapseDailyBySurface, localDateLabel, resolveDailyCurrency } from '@/lib/channel'
import type { ChannelDailyEntry } from '@/api/channel'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

ChartJS.register(LineElement, PointElement, LinearScale, CategoryScale, Tooltip, Legend)

const props = defineProps<{
  daily: ChannelDailyEntry[]
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()
const theme = useThemeStore()

const rows = computed(() => collapseDailyBySurface(props.daily))
const currencyState = computed(() => resolveDailyCurrency(props.daily))
const moneyCurrency = computed(() => (currencyState.value.kind === 'known' ? currencyState.value.currency : undefined))
const showRevenue = computed(() => currencyState.value.kind !== 'mixed')

function tokenColor(name: string): string {
  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function') return '#888888'
  const v = getComputedStyle(document.body).getPropertyValue(name).trim()
  return v || '#888888'
}

// Chart.js draws to a <canvas>, which can't resolve var(--token) itself, so
// the actual token values are read from the DOM at mount and on every theme
// change (R: theme tokens only, correct in both paper and midnight) — no
// hex literal lives in this file's source.
const colors = ref({ ink: '', muted: '', accent: '', ok: '', line: '' })
function refreshColors() {
  colors.value = {
    ink: tokenColor('--ink'),
    muted: tokenColor('--muted'),
    accent: tokenColor('--accent'),
    ok: tokenColor('--ok'),
    line: tokenColor('--line'),
  }
}
onMounted(refreshColors)
watch(() => theme.theme, refreshColors)

const chartData = computed(() => {
  const datasets: ChartDataset<'line'>[] = [
    {
      label: t('channel.trend.initiated'),
      data: rows.value.map((r) => r.checkouts_initiated),
      borderColor: colors.value.muted,
      backgroundColor: colors.value.muted,
      yAxisID: 'y',
      tension: 0.25,
      pointRadius: 2,
    },
    {
      label: t('channel.trend.completed'),
      data: rows.value.map((r) => r.checkouts_completed),
      borderColor: colors.value.accent,
      backgroundColor: colors.value.accent,
      yAxisID: 'y',
      tension: 0.25,
      pointRadius: 2,
    },
  ]
  if (showRevenue.value) {
    datasets.push({
      label: t('channel.trend.revenue'),
      data: rows.value.map((r) => r.revenue_minor),
      borderColor: colors.value.ok,
      backgroundColor: colors.value.ok,
      yAxisID: 'y1',
      tension: 0.25,
      pointRadius: 2,
      borderDash: [5, 4],
    })
  }
  return { labels: rows.value.map((r) => localDateLabel(r.date)), datasets }
})

const chartOptions = computed<ChartOptions<'line'>>(() => {
  const scales: ChartOptions<'line'>['scales'] = {
    x: { ticks: { color: colors.value.muted }, grid: { color: colors.value.line } },
    y: {
      type: 'linear',
      position: 'left',
      beginAtZero: true,
      ticks: { color: colors.value.muted },
      grid: { color: colors.value.line },
    },
  }
  if (showRevenue.value) {
    scales.y1 = {
      type: 'linear',
      position: 'right',
      beginAtZero: true,
      ticks: {
        color: colors.value.muted,
        callback: (value) => formatMinorUnits(Number(value), moneyCurrency.value),
      },
      grid: { display: false },
    }
  }
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    scales,
    plugins: {
      legend: { labels: { color: colors.value.ink } },
      tooltip: {
        callbacks: {
          label: (ctx: TooltipItem<'line'>) => {
            const y = typeof ctx.parsed.y === 'number' ? ctx.parsed.y : 0
            if (ctx.dataset.yAxisID === 'y1') {
              return `${ctx.dataset.label}: ${formatMinorUnits(y, moneyCurrency.value)}`
            }
            return `${ctx.dataset.label}: ${y}`
          },
        },
      },
    },
  }
})
</script>

<template>
  <div class="trend card">
    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />
    <div v-if="loading" class="skel-block">
      <Skeleton height="280px" radius="var(--radius)" />
    </div>
    <template v-else>
      <div class="trend-head">
        <h2 class="trend-title display">{{ t('channel.trend.title') }}</h2>
        <span class="utc-note">{{ t('channel.trend.utcNote') }}</span>
      </div>
      <p v-if="!showRevenue" class="mixed-note">{{ t('channel.trend.mixedCurrency') }}</p>
      <div v-if="!rows.length" class="empty-note">{{ t('channel.trend.empty') }}</div>
      <div v-else class="chart-wrap">
        <Line :data="chartData" :options="chartOptions" role="img" :aria-label="t('channel.trend.title')" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 22px 24px;
}
.trend-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.trend-title { font-size: 18px; }
.utc-note { font-size: 12px; color: var(--faint); }
.mixed-note { font-size: 12.5px; font-weight: 600; color: var(--warn); margin: -8px 0 12px; }
.chart-wrap { height: 280px; position: relative; }
.empty-note { padding: 32px; text-align: center; color: var(--muted); }
.skel-block { padding: 0; }
</style>
