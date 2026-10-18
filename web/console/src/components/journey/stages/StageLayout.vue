<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The Path's body, at every stage of store activation.
//
// One component for every stage. What differs between them is data, not
// structure — see stages.ts — so the structure lives here once: headline,
// the journey bar, whatever stat tiles the stage shows, the task feed, and
// the drawer over the top of it. Six separate files each carried their own
// copy of this and its stylesheet, which is how one of them came to ship
// mockup placeholder text and another a headline belonging to a different
// stage.
//
// Self-loads the catalog only for the stages whose headline needs it
// (synced's product count, ready's score) — the same self-loading pattern
// as ReadinessPill.vue, rather than threading catalog data through
// PathSurface for the four stages that have no use for it.

import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import JourneyBar from '@/components/journey/JourneyBar.vue'
import TaskFeed from '@/components/task/TaskFeed.vue'
import TaskDrawer from '@/components/task/TaskDrawer.vue'
import StatStrip from '@/components/journey/stats/StatStrip.vue'
import { useCatalogStore } from '@/stores/catalog'
import { useStoresStore } from '@/stores/stores'
import { buildStages, PENDING } from './stages'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']
type Store = components['schemas']['store']
type UCPState = components['schemas']['ucp_state']

const props = defineProps<{
  stage: UCPState
  /**
   * Nullable because the store list may not have landed yet — the header
   * block is skipped until it does. The six components this replaced all
   * declared it non-null and got away with it only because `<component :is>`
   * doesn't typecheck its bindings.
   */
  store: Store | null | undefined
  tasks: Task[]
  hasMore: boolean
  loadingMore: boolean
  openTask: Task | null
}>()

const emit = defineEmits<{
  open: [task: Task]
  loadMore: []
  close: []
  resolve: [task: Task]
}>()

const stores = useStoresStore()
const catalog = useCatalogStore()
const { t } = useI18n()

const STAGES = computed(() => buildStages(t))
const def = computed(() => STAGES.value[props.stage])

async function ensureLoaded() {
  if (!def.value.needsCatalog) return
  if (!stores.currentStore) return
  const stale = catalog.readiness == null || catalog.storeId !== stores.currentStoreId
  if (stale && !catalog.loading) {
    await catalog.load()
  }
}
onMounted(ensureLoaded)
// The journey stream can advance the stage under an open session, so a
// stage that needs the catalog may arrive after mount.
watch(() => [stores.currentStoreId, props.stage], ensureLoaded)

const headline = computed(() =>
  def.value.headline({
    storeName: props.store?.name ?? t('task.widgets.catalogSync.yourStoreFallback'),
    productCount: catalog.readiness?.product_count ?? PENDING,
    readinessScore: catalog.readiness?.overall ?? PENDING,
  }),
)
</script>

<template>
  <div v-if="store" class="page-head">
    <div class="eyebrow">{{ store.name }} · {{ store.platform }}</div>
    <h1 class="page-title display">{{ headline }}</h1>
    <p class="page-sub">{{ def.sub }}</p>
  </div>

  <JourneyBar :current="stage" />

  <StatStrip v-if="def.stats.length > 0" :label="def.statsLabel" :stats="def.stats" />

  <TaskFeed
    :tasks="tasks"
    :nothing-needed="def.nothingNeeded"
    @open="(task: Task) => emit('open', task)"
  />

  <div v-if="hasMore" class="load-more">
    <button type="button" class="load-more-btn" :disabled="loadingMore" @click="emit('loadMore')">
      {{ loadingMore ? t('journey.loading') : t('journey.loadMoreTasks') }}
    </button>
  </div>

  <TaskDrawer
    v-if="openTask"
    :task="openTask"
    @close="emit('close')"
    @resolve="(task: Task) => emit('resolve', task)"
  />
</template>

<style scoped>
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub {
  color: var(--muted);
  font-size: 16px;
  margin-top: 8px;
  max-width: 60ch;
}

.load-more { display: flex; justify-content: center; margin-top: 18px; }
.load-more-btn {
  padding: 9px 20px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
  color: var(--ink);
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;
  transition: box-shadow 0.18s, transform 0.18s;
}
.load-more-btn:hover:not(:disabled) { box-shadow: var(--shadow-lg); transform: translateY(-1px); }
.load-more-btn:disabled { opacity: 0.6; cursor: default; }
</style>
