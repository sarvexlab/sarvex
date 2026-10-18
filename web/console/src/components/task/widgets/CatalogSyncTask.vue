<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Catalog sync — the bespoke rendering for source=catalog_import tasks.
//
// Two shapes arrive here. The journey raises the in-progress milestone
// ("Importing your catalog", owner=sarvex) on entering Connected and
// resolves it on Synced, so the done card is the same task after the sync
// landed. The sync plane separately raises an import *failure* task
// (owner=merchant, raiseImportFailedTask) when an import gives up.
//
// The spine supplies title, why, owner and state — this widget only adds
// what the spine has no field for: the product count from
// GET /catalog/readiness and when the sync actually landed, from the
// connector.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import { useStoresStore } from '@/stores/stores'
import { useConnectorsStore } from '@/stores/connectors'
import { useCatalogStore } from '@/stores/catalog'
import { formatFriendlyDateTime } from '@/lib/datetime'
import Chip from '@/components/ui/Chip.vue'
import TaskCardShell from '../TaskCardShell.vue'
import { useWidgetData } from './useWidgetData'
import WorkingBar from './WorkingBar.vue'

type Task = components['schemas']['task']

const props = defineProps<{ task: Task }>()
const emit = defineEmits<{ open: [task: Task] }>()

const stores = useStoresStore()
const connectors = useConnectorsStore()
const catalog = useCatalogStore()
const { t } = useI18n()

useWidgetData(async () => {
  if (connectors.connectors.length === 0 && !connectors.loading) await connectors.load()
  const stale = catalog.readiness == null || catalog.storeId !== stores.currentStoreId
  if (stale && !catalog.loading) await catalog.load()
})

const connector = computed(() => {
  const id = stores.currentStore?.store_id
  return id ? connectors.forStore(id) : null
})
const lastSync = computed(() => connector.value?.last_full_sync_at ?? null)
const productCount = computed(() => catalog.readiness?.product_count ?? null)
const countLabel = computed(() =>
  productCount.value == null ? null : productCount.value.toLocaleString(),
)

// Owner is the discriminator, not state: the failure task belongs to the
// merchant, the import milestone to Sarvex.
const failed = computed(() => props.task.owner === 'merchant')
const done = computed(() => props.task.state === 'done')
const importing = computed(() => !failed.value && !done.value)

// The done milestone keeps the server's title ("Importing your catalog"),
// which reads oddly once it's finished — swap in the count the design asks
// for when we have it.
const title = computed(() => {
  if (!done.value || failed.value) return props.task.title ?? ''
  return countLabel.value
    ? t('task.widgets.catalogSync.firstSyncTitle', { count: countLabel.value })
    : t('task.widgets.catalogSync.firstSyncCompleteTitle')
})

// The spine's copy is count-free (the backend doesn't know it at raise
// time); fill it in when the catalog has loaded.
const why = computed(() => {
  if (!importing.value) return props.task.why_it_matters
  return countLabel.value
    ? t('task.widgets.catalogSync.readingProducts', {
        count: countLabel.value,
        platform: stores.currentStore?.platform ?? t('task.widgets.catalogSync.yourStoreFallback'),
      })
    : props.task.why_it_matters
})
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
      <span v-if="failed">{{ t('task.widgets.catalogSync.needsAttention') }}</span>
      <span v-else-if="importing">{{ t('task.widgets.catalogSync.automatic') }}</span>
      <template v-else>
        <span>{{ t('task.widgets.catalogSync.completed', { when: formatFriendlyDateTime(lastSync, 'recently') }) }}</span>
        <template v-if="countLabel">
          <span class="dot-sep" />
          <span class="mono">{{ t('task.widgets.catalogSync.productsCount', { count: countLabel }) }}</span>
        </template>
      </template>
    </template>

    <template #side>
      <Chip v-if="failed" kind="danger">{{ t('task.widgets.catalogSync.importFailed') }}</Chip>
      <WorkingBar v-else-if="importing" :label="t('task.widgets.catalogSync.importingLabel')" />
    </template>
  </TaskCardShell>
</template>
