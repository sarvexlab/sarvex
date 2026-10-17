<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Single board card. Click → enter-as-merchant.
//
// Counts: per-owner pills (you / us / external) when there's work in flight,
// or a single "Healthy" / "Blocked" / "Stale" chip otherwise.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'

type Card = components['schemas']['board_card']

const props = defineProps<{ card: Card }>()
const emit = defineEmits<{ open: [card: Card] }>()

const { t } = useI18n()

const counts = computed(() => {
  const c = props.card.open_task_counts
  return {
    you: c?.merchant ?? 0,
    us: c?.sarvex ?? 0,
    external: c?.external ?? 0,
  }
})
const totalOpen = computed(() => counts.value.you + counts.value.us + counts.value.external)
</script>

<template>
  <button
    type="button"
    :class="['card', 'mcard', { blocked: card.blocked, stale: !card.blocked && card.stale }]"
    @click="emit('open', card)"
  >
    <div class="mname">{{ card.name }}</div>
    <div class="mid mono">{{ card.merchant_id }}</div>

    <div class="mtasks">
      <template v-if="totalOpen === 0 && !card.blocked && !card.stale">
        <Chip kind="ok"><Icon name="check" /> {{ t('operator.card.healthy') }}</Chip>
      </template>
      <template v-else>
        <Chip v-if="card.blocked" kind="danger">{{ t('operator.card.blocked') }}</Chip>
        <Chip v-else-if="card.stale" kind="warn">{{ t('operator.card.stale') }}</Chip>
        <span v-if="counts.you > 0" class="owncount you">
          <Icon name="user" /> {{ counts.you }}
        </span>
        <span v-if="counts.us > 0" class="owncount us">
          <Icon name="spark" /> {{ counts.us }}
        </span>
        <span v-if="counts.external > 0" class="owncount external">
          <Icon name="external" /> {{ counts.external }}
        </span>
      </template>
    </div>

    <div class="mdays">{{ t('operator.card.dayInState', { state: card.state, day: card.days_in_state ?? 0 }) }}</div>
  </button>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.mcard {
  display: block;
  width: 100%;
  text-align: left;
  padding: 15px;
  cursor: pointer;
  position: relative;
  transition: box-shadow 0.16s, transform 0.16s;
  overflow: hidden;
  font: inherit;
  color: inherit;
}
.mcard:hover { box-shadow: var(--shadow-lg); transform: translateY(-2px); }
.mcard.blocked { box-shadow: inset 3px 0 0 var(--danger), var(--shadow); }
.mcard.stale   { box-shadow: inset 3px 0 0 var(--warn), var(--shadow); }

.mname { font-weight: 600; font-size: 15px; }
.mid { font-size: 11.5px; color: var(--faint); margin-top: 2px; }

.mtasks {
  display: flex;
  gap: 6px;
  margin-top: 12px;
  flex-wrap: wrap;
}
.mdays { font-size: 12px; color: var(--muted); margin-top: 12px; }

.owncount {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 99px;
}
.owncount.you    { background: var(--accent-soft); color: var(--accent-deep); }
.owncount.us     { background: var(--surface2);    color: var(--muted); }
.owncount.external { background: transparent; color: var(--faint); box-shadow: inset 0 0 0 1px var(--line2); }
.owncount :deep(svg) { width: 11px; height: 11px; }
</style>
