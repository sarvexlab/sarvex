<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The owner-grouped task feed. Used as-is on the Path; reusable on the
// Board's enter-as-merchant view in step 7 with the same grouping shape.
//
// Each row resolves through the widget registry first: a named task gets its
// bespoke widget, everything else — which is most tasks, since they're
// created dynamically at runtime — gets the generic TaskCard. Either way it
// is the same spine: the feed shows every task and nothing but tasks.

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import TaskCard from './TaskCard.vue'
import Icon from '@/components/ui/Icon.vue'
import { widgetForTask } from './widgets/registry'
import { doneTasks, openFor } from '@/lib/task'

type Task = components['schemas']['task']

const { t } = useI18n()

const props = defineProps<{
  tasks: Task[]
  asOperator?: boolean
  storeName?: string
  /**
   * What to say under "Nothing needed from you". The Path passes its
   * stage's line (see stages.ts); the default covers callers with no
   * stage context, like the Board. It used to be hardcoded to "Next
   * milestone: sandbox validation", which every merchant saw at every
   * stage — including after going live.
   */
  nothingNeeded?: string
}>()

const nothingNeededText = computed(() => props.nothingNeeded ?? t('task.feed.nothingNeededDefault'))

const emit = defineEmits<{
  open: [task: Task]
  op: [action: 'apply' | 'note' | 'reassign' | 'done', task: Task]
}>()

const showDone = ref(false)

/**
 * The component a row renders through — its widget, or the generic card.
 *
 * Multi-store contexts (the Board's feed, which labels each row with its
 * store) always get the generic card: the widgets self-load against the
 * *current* store, so specialising a row that belongs to a different store
 * would show the wrong store's data.
 */
function cardFor(task: Task) {
  if (props.storeName) return TaskCard
  return widgetForTask(task) ?? TaskCard
}

/**
 * Widgets take only `task` — operator actions and the store tag are generic
 * card concerns, and passing them through would land as stray attributes.
 */
function bindingsFor(task: Task) {
  return cardFor(task) === TaskCard
    ? { task, asOperator: props.asOperator, storeName: props.storeName }
    : { task }
}

// Owner mapping to design's visual states: merchant = "you", sarvex = "us",
// external = waiting on an external party.
// Done collapses into its own section regardless of owner. Grouped through
// @/lib/task so these sections and the tasks store's getters can't drift
// apart.
const youTasks = computed(() => openFor(props.tasks, 'merchant'))
const usTasks = computed(() => openFor(props.tasks, 'sarvex'))
const externalTasks = computed(() => openFor(props.tasks, 'external'))
const finishedTasks = computed(() => doneTasks(props.tasks))
</script>

<template>
  <div class="feed-root">
    <!-- Needs you now -->
    <div class="section-label">
      {{ t('task.feed.needsYouNow') }} <span class="count">· {{ youTasks.length }}</span>
      <span class="ln" />
    </div>
    <div v-if="youTasks.length === 0" class="card empty-note">
      <div class="big">{{ t('task.feed.nothingNeededTitle') }}</div>
      {{ nothingNeededText }}
    </div>
    <div v-else class="task-feed">
      <component
        :is="cardFor(t)"
        v-for="t in youTasks"
        :key="t.task_id"
        v-bind="bindingsFor(t)"
        @open="(task: Task) => emit('open', task)"
        @op="(action: 'apply' | 'note' | 'reassign' | 'done', task: Task) => emit('op', action, task)"
      />
    </div>

    <!-- What we're doing for you -->
    <div class="section-label">
      {{ t('task.feed.whatWereDoing') }} <span class="count">· {{ usTasks.length }}</span>
      <span class="ln" />
    </div>
    <div class="task-feed">
      <component
        :is="cardFor(t)"
        v-for="t in usTasks"
        :key="t.task_id"
        v-bind="bindingsFor(t)"
        @open="(task: Task) => emit('open', task)"
        @op="(action: 'apply' | 'note' | 'reassign' | 'done', task: Task) => emit('op', action, task)"
      />
    </div>

    <!-- Waiting on an external party -->
    <template v-if="externalTasks.length > 0">
      <div class="section-label">
        {{ t('task.feed.waitingOnExternal') }} <span class="count">· {{ externalTasks.length }}</span>
        <span class="ln" />
      </div>
      <div class="task-feed">
        <component
          :is="cardFor(t)"
          v-for="t in externalTasks"
          :key="t.task_id"
          v-bind="bindingsFor(t)"
          @open="(task: Task) => emit('open', task)"
          @op="(action: 'apply' | 'note' | 'reassign' | 'done', task: Task) => emit('op', action, task)"
        />
      </div>
    </template>

    <!-- Done (collapsed) -->
    <button class="section-label section-toggle" type="button" @click="showDone = !showDone">
      <span class="toggle-inner">
        <span :class="['chev', { open: showDone }]"><Icon name="chevD" /></span>
        {{ t('task.feed.done') }} <span class="count">· {{ finishedTasks.length }}</span>
      </span>
      <span class="ln" />
    </button>
    <div v-if="showDone" class="task-feed">
      <component
        :is="cardFor(t)"
        v-for="t in finishedTasks"
        :key="t.task_id"
        v-bind="bindingsFor(t)"
        @open="(task: Task) => emit('open', task)"
        @op="(action: 'apply' | 'note' | 'reassign' | 'done', task: Task) => emit('op', action, task)"
      />
    </div>
  </div>
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
.section-label .count { color: var(--accent-deep); }

.section-toggle {
  background: none;
  border: 0;
  padding: 0;
  cursor: pointer;
  width: 100%;
  color: var(--faint);
}
.section-toggle .toggle-inner {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.section-toggle .chev {
  display: inline-flex;
  width: 14px;
  height: 14px;
  transform: rotate(-90deg);
  transition: transform 0.2s;
}
.section-toggle .chev.open { transform: rotate(0); }

.task-feed { display: flex; flex-direction: column; gap: var(--gap-feed); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.empty-note {
  text-align: center;
  padding: 40px 20px;
  color: var(--muted);
}
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 22px;
  color: var(--ink);
  margin-bottom: 8px;
  letter-spacing: var(--display-tracking);
}
</style>
