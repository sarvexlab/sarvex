<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The task-spine primitive: what / why / owner / how it resolves / state.
// This is the *generic* rendering — the fallback for any task the widget
// registry (./widgets/registry.ts) doesn't recognise, which is every
// dynamically-created task. Named, well-known tasks render through their
// own widget instead; both go through TaskCardShell so they share one
// visual language.
//
// The `task` prop is typed straight from the OpenAPI; mapping to the
// design's visual states (owner you/us/external, the resolution label, the
// owner-coloured leading dot) happens inside TaskCardShell.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import TaskCardShell from './TaskCardShell.vue'

type Task = components['schemas']['task']
type Owner = components['schemas']['task_owner']
type Resolution = components['schemas']['task_resolution']

// The OpenAPI doesn't mark task fields as required, so the generated type
// makes everything optional. The server always returns the spine fields,
// so narrow them once here and consume the narrowed task below.
type LoadedTask = Task & {
  owner: Owner
  resolution: Resolution
  state: NonNullable<Task['state']>
  title: string
}

const props = defineProps<{
  task: Task
  /** Show inline operator actions (Apply / Annotate / Reassign / Mark done). */
  asOperator?: boolean
  /** Multi-store contexts (board, email) pass the store name in to label the card. */
  storeName?: string
  /** Suppresses click + chevron when the card is a static preview. */
  readOnly?: boolean
}>()

const emit = defineEmits<{
  open: [task: Task]
  op: [action: 'apply' | 'note' | 'reassign' | 'done', task: Task]
}>()

const { t } = useI18n()
const tk = computed(() => props.task as LoadedTask)
const isDone = computed(() => tk.value.state === 'done')

const resolutionLabelKey: Record<Resolution, string> = {
  one_click:     'task.resolution.one_click',
  guided:        'task.resolution.guided',
  automatic:     'task.resolution.automatic',
  external_wait: 'task.resolution.external_wait',
}
</script>

<template>
  <TaskCardShell
    :owner="tk.owner"
    :state="tk.state"
    :title="tk.title"
    :why="tk.why_it_matters"
    :store-name="storeName"
    :read-only="readOnly"
    @activate="emit('open', task)"
  >
    <template #meta>
      <span v-if="isDone">{{ t('task.completed') }}</span>
      <span v-else>{{ t(resolutionLabelKey[tk.resolution]) }}</span>
    </template>

    <template #body>
      <div v-if="asOperator && !isDone && !readOnly" class="op-actions" @click.stop>
        <button v-if="tk.resolution === 'one_click'" @click="emit('op', 'apply', task)">
          <Icon name="apply" /> {{ t('task.actions.apply') }}
        </button>
        <button @click="emit('op', 'note', task)"><Icon name="note" /> {{ t('task.actions.annotate') }}</button>
        <button @click="emit('op', 'reassign', task)"><Icon name="swap" /> {{ t('task.actions.reassign') }}</button>
        <button @click="emit('op', 'done', task)"><Icon name="check" /> {{ t('task.actions.markDone') }}</button>
      </div>
    </template>

    <!--
      One chip per state, so the card's right-hand side always answers the
      same question: what is happening to this task right now. `in_progress`
      previously showed nothing at all, making it indistinguishable from
      `open` — the merchant could not tell work Sarvex had already started
      from work that was waiting on them.
    -->
    <template #side>
      <Chip v-if="tk.state === 'blocked'" kind="quiet">
        <Icon name="clock" /> {{ t('task.state.waiting') }}
      </Chip>
      <Chip v-else-if="tk.state === 'in_progress'" kind="quiet">
        <Icon name="spark" /> {{ t('task.state.working') }}
      </Chip>
      <Chip v-else-if="tk.state === 'open' && tk.resolution === 'one_click'" kind="you">
        {{ t('task.state.oneClick') }}
      </Chip>
    </template>
  </TaskCardShell>
</template>

<style scoped>
.op-actions {
  display: flex;
  gap: 2px;
  margin-top: 11px;
  flex-wrap: wrap;
  padding-top: 11px;
  border-top: 1px dashed var(--line2);
}
.op-actions button {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-deep);
  padding: 4px 9px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: 0;
  cursor: pointer;
}
.op-actions button:hover { background: var(--accent-soft); }
.op-actions :deep(svg) { width: 13px; height: 13px; }
</style>
