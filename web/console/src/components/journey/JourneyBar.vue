<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The activation progress bar. Stages and labels come from the OpenAPI
// ucp_state enum (single source of truth — changing the enum is the only
// way to change what's rendered).
//
// It ends at `active`, and deliberately so: the bar tracks the store's own
// activation, which is complete once its endpoints are live and monitored.
// It used to run on journey_state and end at "Live on Google", which made
// every merchant's progress bar terminate at a distribution partner they
// might never enroll with. `suspended` is absent for the same reason it is
// absent from activation.Order(): it is a condition, not a step.
//
// `current` is where the store is now; `selected` (optional) is the stage the
// caller is previewing (PathSurface uses this for the stage-rewind banner).
// When `selectable`, clicking a step emits `select` with the stage id.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'

type State = components['schemas']['ucp_state']

const props = defineProps<{
  current: State
  selected?: State | null
  selectable?: boolean
}>()

const emit = defineEmits<{ select: [stage: State] }>()

export type Stage = { id: State; label: string }

const { t } = useI18n()

// Short labels paired to the OpenAPI enum 1:1, in progression order.
const STAGE_IDS: State[] = [
  'draft',
  'connector_linked',
  'profile_published',
  'verified',
  'active',
]
const STAGES = computed<Stage[]>(() =>
  STAGE_IDS.map((id) => ({ id, label: t(`journey.stage.${id}`) })),
)

const curIdx = computed(() => STAGES.value.findIndex((s) => s.id === props.current))
const sel = computed<State>(() => props.selected ?? props.current)
</script>

<template>
  <div class="journey">
    <component
      :is="selectable ? 'button' : 'div'"
      v-for="(s, i) in STAGES"
      :key="s.id"
      :type="selectable ? 'button' : undefined"
      :title="selectable ? t('journey.stageSeeAt', { label: s.label }) : undefined"
      :class="[
        'jstep',
        i < curIdx ? 'done' : i === curIdx ? 'current' : 'todo',
        { selected: s.id === sel, tap: selectable },
      ]"
      @click="selectable ? emit('select', s.id) : undefined"
    >
      <span class="jline l" />
      <span class="jline r" />
      <span v-if="i === curIdx" class="jflag" aria-hidden="true">📍</span>
      <span class="jnode" />
      <span class="jlabel">{{ s.label }}</span>
    </component>
  </div>
</template>

<style scoped>
.journey {
  display: flex;
  align-items: stretch;
  gap: 0;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 6px;
  box-shadow: var(--shadow);
}
.jstep {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 14px 6px 12px;
  border-radius: var(--radius);
  position: relative;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
}
.jstep .jnode {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--line2);
  box-shadow: 0 0 0 4px var(--surface);
  z-index: 2;
}
.jstep.done .jnode    { background: var(--ok); }
.jstep.current .jnode {
  background: var(--accent);
  box-shadow: 0 0 0 4px var(--accent-soft);
}
.jstep .jlabel {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--faint);
  letter-spacing: 0.01em;
}
.jstep.done .jlabel    { color: var(--muted); }
.jstep.current         { background: var(--accent-softer); }
.jstep.current .jlabel { color: var(--accent-deep); }
.jstep .jline {
  position: absolute;
  top: calc(14px + 5px);
  height: 2px;
  background: var(--line2);
  z-index: 1;
}
.jstep .jline.l { left: 0; right: 50%; }
.jstep .jline.r { left: 50%; right: 0; }
.jstep.done .jline,
.jstep.done .jline.r { background: var(--ok); }
.jstep:first-child .jline.l,
.jstep:last-child  .jline.r { display: none; }
.jstep .jflag { position: absolute; top: 12px; font-size: 13px; }

button.jstep.tap { cursor: pointer; transition: background 0.15s, box-shadow 0.15s; }
button.jstep.tap:hover { background: var(--surface2); }
button.jstep.tap:hover .jlabel { color: var(--ink); }
.jstep.selected:not(.current) {
  background: var(--surface2);
  box-shadow: inset 0 0 0 1.5px var(--accent-line);
}
.jstep.selected:not(.current) .jlabel { color: var(--ink); }
.jstep.current.selected { box-shadow: inset 0 0 0 1.5px var(--accent-line); }
</style>
