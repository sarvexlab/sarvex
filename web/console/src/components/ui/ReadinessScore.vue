<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Ring + percentage. Colour reflects the score band so the ring itself is
// never the only signal — the number and a StatusChip alongside it (caller's
// responsibility) carry the same information in text.
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ score: number; size?: number; strokeWidth?: number }>(),
  { size: 120, strokeWidth: 10 }
)

const radius = computed(() => (props.size - props.strokeWidth) / 2)
const circumference = computed(() => 2 * Math.PI * radius.value)
const clamped = computed(() => Math.max(0, Math.min(100, props.score)))
const offset = computed(() => circumference.value * (1 - clamped.value / 100))

const band = computed(() => {
  if (clamped.value >= 90) return 'pass'
  if (clamped.value >= 60) return 'flag'
  return 'fail'
})
</script>

<template>
  <div
    class="readiness-score"
    :style="{ width: `${size}px`, height: `${size}px` }"
    role="img"
    :aria-label="`Readiness score: ${clamped} out of 100`"
  >
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`">
      <circle
        class="track"
        :cx="size / 2"
        :cy="size / 2"
        :r="radius"
        :stroke-width="strokeWidth"
        fill="none"
      />
      <circle
        :class="['fill', `is-${band}`]"
        :cx="size / 2"
        :cy="size / 2"
        :r="radius"
        :stroke-width="strokeWidth"
        fill="none"
        stroke-linecap="round"
        :stroke-dasharray="circumference"
        :stroke-dashoffset="offset"
        :transform="`rotate(-90 ${size / 2} ${size / 2})`"
      />
    </svg>
    <span class="rs-value display">{{ clamped }}<span class="rs-unit">%</span></span>
  </div>
</template>

<style scoped>
.readiness-score { position: relative; display: inline-grid; place-items: center; }
.readiness-score svg { position: absolute; inset: 0; }
.track { stroke: var(--border); }
.fill { transition: stroke-dashoffset 0.6s var(--ease-out, ease-out); }
.fill.is-pass { stroke: var(--pass); }
.fill.is-flag { stroke: var(--flag); }
.fill.is-fail { stroke: var(--fail); }

.rs-value {
  position: relative;
  font-size: 28px;
  font-weight: 500;
  color: var(--text);
}
.rs-unit { font-size: 15px; color: var(--text-muted); margin-left: 1px; }

@media (prefers-reduced-motion: reduce) {
  .fill { transition: none; }
}
</style>
