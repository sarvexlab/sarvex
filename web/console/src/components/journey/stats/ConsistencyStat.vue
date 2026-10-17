<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// "In sync · Prices & inventory" — the Live stage's standing reassurance.
//
// A readout, not a task. When something *does* drift, the monitoring loop
// raises a real task per finding and it appears in the feed below with its
// finding drill-in; this tile only says how many are open so the merchant
// knows where the number came from.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useHealthStore } from '@/stores/health'
import { formatFriendlyDateTime } from '@/lib/datetime'
import { useNow } from '@/lib/now'
import { useWidgetData } from '@/components/task/widgets/useWidgetData'
import StatTile from './StatTile.vue'

const health = useHealthStore()
const { t } = useI18n()

// Same fix as the Health page (see HealthSurface.vue): formatFriendlyDateTime
// renders "2 minutes ago" from a fixed input, so a computed that calls it
// once shows a phrase that never moves. Reading the ticking clock inside the
// computed is what makes Vue re-run it.
const now = useNow()

useWidgetData(async () => {
  if (!health.loaded && !health.loading) await health.load()
})

const statement = computed(
  () => health.health?.statements?.find((s) => s.kind === 'consistency') ?? null,
)
const findingCount = computed(() => health.findings.length)

// Treat it as consistent unless the statement says otherwise or findings are
// open — a store nothing has been checked on yet hasn't drifted.
const consistent = computed(() => statement.value?.ok !== false && findingCount.value === 0)

const value = computed(() => (consistent.value ? t('journey.consistencyStat.inSync') : String(findingCount.value)))

const label = computed(() =>
  consistent.value ? t('journey.consistencyStat.priceAndInventory') : t('journey.consistencyStat.driftedLabel'),
)

const note = computed(() => {
  if (!consistent.value) {
    return statement.value?.message ?? t('journey.consistencyStat.driftedNoteDefault')
  }
  const checked = statement.value?.checked_at
  if (!checked) return t('journey.consistencyStat.neverChecked')
  void now.value
  return t('journey.consistencyStat.lastChecked', { when: formatFriendlyDateTime(checked) })
})
</script>

<template>
  <StatTile
    :value="value"
    :tone="consistent ? 'ok' : 'warn'"
    :label="label"
    :note="note"
  />
</template>
