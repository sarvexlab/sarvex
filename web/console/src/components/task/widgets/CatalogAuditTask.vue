<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The readiness audit — bespoke rendering for the journey's
// readiness:<store>:initial_audit milestone, raised on entering Synced and
// resolved on Ready.
//
// The spine says whether it's running or finished; GET /catalog/readiness
// adds the score, which is the thing the merchant is actually waiting for
// and which the spine has no field for.
//
// No audit-progress endpoint exists, so the running state uses WorkingBar's
// indeterminate treatment rather than a fabricated percentage.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import { useCatalogStore } from '@/stores/catalog'
import { useStoresStore } from '@/stores/stores'
import { formatFriendlyDateTime } from '@/lib/datetime'
import Chip from '@/components/ui/Chip.vue'
import TaskCardShell from '../TaskCardShell.vue'
import { useWidgetData } from './useWidgetData'
import WorkingBar from './WorkingBar.vue'

type Task = components['schemas']['task']

const props = defineProps<{ task: Task }>()
const emit = defineEmits<{ open: [task: Task] }>()

const catalog = useCatalogStore()
const stores = useStoresStore()
const { t } = useI18n()

useWidgetData(async () => {
  const stale = catalog.readiness == null || catalog.storeId !== stores.currentStoreId
  if (stale && !catalog.loading) await catalog.load()
})

const lastAudit = computed(() => catalog.readiness?.last_audit_at ?? null)
const score = computed(() => catalog.readiness?.overall ?? null)
const auditing = computed(() => props.task.state !== 'done')

const title = computed(() => {
  if (auditing.value) return props.task.title ?? t('task.widgets.catalogAudit.defaultTitle')
  return score.value == null
    ? t('task.widgets.catalogAudit.completeTitle')
    : t('task.widgets.catalogAudit.completeScoredTitle', { score: score.value })
})

const why = computed(() =>
  auditing.value
    ? props.task.why_it_matters
    : t('task.widgets.catalogAudit.completeWhy'),
)
</script>

<template>
  <TaskCardShell
    :owner="task.owner!"
    :state="task.state!"
    :title="title"
    :why="why"
    @activate="emit('open', task)"
  >
    <template #meta>
      <span v-if="auditing">{{ t('task.widgets.catalogAudit.automatic') }}</span>
      <span v-else>{{ t('task.widgets.catalogAudit.lastAudit', { when: formatFriendlyDateTime(lastAudit, 'recently') }) }}</span>
    </template>

    <template #side>
      <WorkingBar v-if="auditing" :label="t('task.widgets.catalogAudit.auditingLabel')" />
      <Chip v-else-if="score != null" kind="ok">{{ score }} / 100</Chip>
    </template>
  </TaskCardShell>
</template>
