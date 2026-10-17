<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The merchant's home. Layout, data sources, and the resolve flow are
// unchanged from step 4; step 8 adds:
//   - Deep linking: /path?task=<id> opens the drawer for that task and
//     bidirectional URL sync (drawer open/close updates the query).
//   - Loading skeleton for the headline + journey + task feed.
//   - Error state with retry for the tasks/journey loads.

import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useStoresStore } from '@/stores/stores'
import { useJourneyStore } from '@/stores/journey'
import { useActivationStore } from '@/stores/activation'
import { useTasksStore } from '@/stores/tasks'
import StageLayout from '@/components/journey/stages/StageLayout.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']

const route = useRoute()
const router = useRouter()
const stores = useStoresStore()
// The journey store still backs the realtime task stream; the Path's own
// stage now comes from activation. The two were the same thing until
// activation and surface enrollment were split — journey_state's last three
// values were Google's, so keying the Path on it meant a merchant who never
// enrolled with Google could not reach the end of their own onboarding.
const journey = useJourneyStore()
const activation = useActivationStore()
const tasks = useTasksStore()

// Deep link: the query param drives which drawer is open, resolved against
// the store on every read rather than snapshotted. It used to be a ref
// assigned once per navigation, which meant a live task.updated push —
// which replaces the array entry with a freshly fetched object — left the
// open drawer rendering the task as it looked when it was opened.
// Deriving it also covers back-button navigation and the email-link case
// where the drawer is asked for before the list has arrived.
const openTask = computed<Task | null>(() => {
  const id = (route.query.task as string | undefined) ?? null
  if (!id) return null
  return tasks.tasks.find((x) => x.task_id === id) ?? null
})

async function refresh() {
  await Promise.all([
    stores.loaded ? Promise.resolve() : stores.load(),
    journey.load(),
    activation.load(),
    tasks.load(stores.currentStoreId ? { store_id: stores.currentStoreId } : {}),
  ])
}
onMounted(refresh)
watch(() => stores.currentStoreId, refresh)
// The live stream is the shell's (AppShell.vue) — it must outlive this
// surface, or every other page is frozen at load.

const store = computed(() => stores.currentStore)

const showSkeleton = computed(() => !tasks.loaded && tasks.loading)
const loadError = computed(() => tasks.error ?? activation.error ?? journey.error ?? null)

function onOpen(task: Task) {
  if (!task.task_id) return
  // Sync the URL so the drawer is shareable / back-button-aware.
  router.replace({ query: { ...route.query, task: task.task_id } })
}

function onClose() {
  const { task: _drop, ...rest } = route.query
  router.replace({ query: rest })
}

async function onResolve(task: Task) {
  if (!task.task_id) return
  await tasks.resolve(task.task_id)
  // Resolving changes the open counts that drive the headline and may have
  // been the last thing holding an activation step; refetch both so the bar
  // reflects the new state.
  await Promise.all([journey.load(), activation.load()])
  onClose()
}
</script>

<template>
  <div class="page">
    <ErrorBanner
      v-if="loadError"
      :message="loadError"
      @retry="refresh"
    />

    <!-- Loading skeleton -->
    <template v-if="showSkeleton">
      <div class="page-head">
        <div class="eyebrow"><Skeleton width="220px" height="12px" /></div>
        <h1 class="page-title display skel-stack">
          <Skeleton width="80%" height="34px" />
          <Skeleton width="62%" height="34px" />
        </h1>
        <p class="page-sub"><Skeleton width="56%" height="16px" /></p>
      </div>

      <div class="skel-journey">
        <Skeleton v-for="i in 5" :key="i" height="48px" radius="10px" />
      </div>

      <div class="skel-feed">
        <div class="skel-section"><Skeleton width="160px" height="12px" /></div>
        <Skeleton v-for="i in 3" :key="i" height="78px" radius="11px" />
      </div>
    </template>

    <!-- Loaded -->
    <template v-else>
      <StageLayout
        v-if="activation.activation?.ucp_state"
        :stage="activation.activation.ucp_state"
        :store="store"
        :tasks="tasks.tasks"
        :has-more="tasks.hasMore"
        :loading-more="tasks.loadingMore"
        :open-task="openTask"
        @open="onOpen"
        @load-more="tasks.loadMore()"
        @close="onClose"
        @resolve="onResolve"
      />
    </template>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub {
  color: var(--muted);
  font-size: 16px;
  margin-top: 8px;
  max-width: 60ch;
}

.skel-stack { display: flex; flex-direction: column; gap: 8px; }
.skel-journey {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 6px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 6px;
  box-shadow: var(--shadow);
}
.skel-feed {
  display: flex;
  flex-direction: column;
  gap: var(--gap-feed);
  margin-top: 34px;
}
.skel-section { margin: 0 4px 4px; }
</style>
