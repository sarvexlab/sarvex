<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The card chrome every task-shaped thing on the Path renders through:
// owner-coloured leading dot, title, why, meta row, and a right-hand side
// region. Extracted from TaskCard so the per-task widgets in ./widgets can
// diverge in content without re-implementing (or drifting from) the visual
// language.
//
// TaskCard stays the generic consumer — a task the widget registry doesn't
// recognise still renders exactly as it did before.

import { computed } from 'vue'
import type { components } from '@/api/schema'
import Icon, { type IconName } from '@/components/ui/Icon.vue'
import OwnerChip from '@/components/ui/OwnerChip.vue'

type Owner = components['schemas']['task_owner']
type State = NonNullable<components['schemas']['task']['state']>

const props = defineProps<{
  owner: Owner
  state: State
  title: string
  why?: string
  /** Multi-store contexts (board, email) label the card with its store. */
  storeName?: string
  /** Suppresses click + chevron when the card is a static preview. */
  readOnly?: boolean
}>()

const emit = defineEmits<{ activate: [] }>()

const isDone = computed(() => props.state === 'done')

// Owner says whose task it is — the leading dot, and nothing else.
//
// 'external' is a task waiting on an outside party; the map stays total over
// Owner and renders a generic "external party" treatment.
const ownerView: Record<Owner, { cls: string; icon: IconName }> = {
  merchant: { cls: 'owner-you',      icon: 'user'  },
  sarvex:   { cls: 'owner-us',       icon: 'spark' },
  external:   { cls: 'owner-external', icon: 'clock' },
}
const view = computed(() => ownerView[props.owner])

// State says what's happening to it — the card treatment.
//
// This used to key off owner, which meant the two could disagree: a
// Google-owned checklist task was raised `open` but drew the dashed
// waiting card anyway, while an `in_progress` task was indistinguishable
// from an `open` one. State is now derived server-side from owner and
// resolution (domain.DeriveTaskState), so keying the visual off it gives
// one appearance per situation instead of two half-answers.
const stateView: Record<State, string> = {
  open:        '',            // solid card: someone has to act
  in_progress: 'quiet-card',  // we're on it
  blocked:     'wait-card',   // dashed: waiting on someone else
  done:        'wait-card is-done',
}
const cardClass = computed(() => stateView[props.state])

function onClick() {
  if (props.readOnly || isDone.value) return
  emit('activate')
}
</script>

<template>
  <div :class="['card', 'task-card', cardClass, { ro: readOnly }]" @click="onClick">
    <div class="task-lead">
      <div :class="['owner-dot', view.cls]">
        <slot name="lead"><Icon :name="isDone ? 'check' : view.icon" /></slot>
      </div>
    </div>

    <div class="task-main">
      <div class="task-title">{{ title }}</div>
      <div v-if="why" class="task-why">{{ why }}</div>

      <div class="task-meta">
        <template v-if="storeName">
          <span class="store-tag"><Icon name="store" />{{ storeName }}</span>
          <span class="dot-sep" />
        </template>
        <template v-if="!isDone">
          <OwnerChip :owner="owner" />
          <span class="dot-sep" />
        </template>
        <slot name="meta" />
      </div>

      <slot name="body" />
    </div>

    <div class="task-side">
      <slot name="side" />
      <span v-if="!isDone && !readOnly" class="chevr"><Icon name="chevR" /></span>
    </div>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.task-card {
  display: flex;
  align-items: flex-start;
  gap: 15px;
  padding: var(--pad-card);
  cursor: pointer;
  text-align: left;
  position: relative;
  transition: box-shadow 0.18s, transform 0.18s;
}
.task-card:hover { box-shadow: var(--shadow-lg); transform: translateY(-1px); }
.task-card.ro { cursor: default; }
.task-card.ro:hover { box-shadow: var(--shadow); transform: none; }

.task-card.is-done { opacity: 0.58; box-shadow: none; }
.task-card.is-done:hover { opacity: 0.85; }

.quiet-card { background: var(--accent-softer); box-shadow: none; }
.wait-card  { background: transparent; border-style: dashed; box-shadow: none; }
.quiet-card.ro:hover, .wait-card.ro:hover { box-shadow: none; }

.task-lead { flex-shrink: 0; margin-top: 2px; }
.owner-dot {
  width: 34px;
  height: 34px;
  border-radius: 9px;
  display: grid;
  place-items: center;
}
.owner-dot :deep(svg) { width: 17px; height: 17px; }
.owner-you    { background: var(--accent-soft); color: var(--accent-deep); }
.owner-us     { background: var(--surface2);    color: var(--muted); }
.owner-external {
  background: transparent;
  color: var(--faint);
  box-shadow: inset 0 0 0 1px var(--line2);
}

.task-main { flex: 1; min-width: 0; }
.task-title {
  font-weight: 600;
  font-size: calc(var(--fs-body) + 1px);
  line-height: 1.35;
}
.task-why {
  color: var(--muted);
  font-size: var(--fs-small);
  margin-top: 4px;
}
.task-meta {
  color: var(--faint);
  font-size: 12px;
  margin-top: 9px;
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
}
.store-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
  color: var(--accent-deep);
}
.store-tag :deep(svg) { width: 12px; height: 12px; }

.task-side {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 9px;
  flex-shrink: 0;
}
.chevr { color: var(--faint); flex-shrink: 0; align-self: center; }
.chevr :deep(svg) { width: 16px; height: 16px; }
</style>
