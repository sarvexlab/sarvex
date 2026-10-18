<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Vertical event timeline (R7). Reconstruct-and-display only — reading what
// already happened, never a re-execution (R10: no retry/re-run anywhere in
// this UI). Each entry's redacted payload is collapsed JSON by default.
//
// Entries whose seq appears in `highlightedSeqs` (the diagnosis's
// evidence[]) get a persistent accent treatment so the evidence linkage
// (R8) reads at a glance, not just on hover/click.

import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatRelativeTime, formatUTCTimestamp } from '@/lib/datetime'
import type { CheckoutSessionTimelineEntry } from '@/api/checkoutSessions'

defineProps<{
  entries: CheckoutSessionTimelineEntry[]
  highlightedSeqs?: Set<number>
}>()

const { t } = useI18n()
const expanded = ref<Set<number>>(new Set())
function toggle(seq: number | undefined) {
  if (seq === undefined) return
  const next = new Set(expanded.value)
  if (next.has(seq)) next.delete(seq)
  else next.add(seq)
  expanded.value = next
}
</script>

<template>
  <ol class="timeline">
    <li
      v-for="e in entries"
      :id="`timeline-entry-${e.seq}`"
      :key="e.seq"
      class="entry"
      :class="{ evidence: e.seq !== undefined && highlightedSeqs?.has(e.seq) }"
    >
      <div class="rail">
        <span class="dot" />
        <span class="line" />
      </div>
      <div class="body">
        <div class="head">
          <span class="event-type mono">{{ e.event_type ?? t('checkout.timeline.unknownEvent') }}</span>
          <span v-if="e.stage" class="stage">{{ e.stage }}</span>
          <span
            v-if="e.seq !== undefined && highlightedSeqs?.has(e.seq)"
            class="evidence-tag"
          >
            {{ t('checkout.timeline.evidence') }}
          </span>
        </div>
        <div class="meta" :title="formatUTCTimestamp(e.occurred_at)">
          <span class="mono seq">seq {{ e.seq }}</span>
          · {{ formatRelativeTime(e.occurred_at) }}
          · <span class="abs">{{ formatUTCTimestamp(e.occurred_at) }}</span>
        </div>
        <button
          v-if="e.payload && Object.keys(e.payload).length > 0"
          type="button"
          class="payload-toggle"
          :aria-expanded="e.seq !== undefined && expanded.has(e.seq)"
          @click="toggle(e.seq)"
        >
          {{ e.seq !== undefined && expanded.has(e.seq) ? t('checkout.timeline.hidePayload') : t('checkout.timeline.showPayload') }}
        </button>
        <pre v-if="e.seq !== undefined && expanded.has(e.seq)" class="payload mono">{{ JSON.stringify(e.payload, null, 2) }}</pre>
      </div>
    </li>
  </ol>
</template>

<style scoped>
.timeline { list-style: none; margin: 0; padding: 0; }
.entry { display: flex; gap: 14px; padding: 0 0 22px; }
.entry:last-child { padding-bottom: 0; }
.entry:last-child .line { display: none; }

.rail { display: flex; flex-direction: column; align-items: center; width: 12px; flex-shrink: 0; }
.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--line2);
  margin-top: 3px;
  flex-shrink: 0;
}
.entry.evidence .dot { background: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.line { flex: 1; width: 1px; background: var(--line); margin-top: 4px; }

.body { flex: 1; min-width: 0; }
.entry.evidence .body {
  background: var(--accent-softer);
  border: 1px solid var(--accent-line);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  margin-top: -6px;
}

/* Jump-to flash (R8): toggled briefly from the parent when a diagnosis
   evidence chip scrolls a timeline entry into view. */
.entry.jump-flash .body { animation: jumpflash 1.6s ease-out; }
@keyframes jumpflash {
  0%, 40% { box-shadow: 0 0 0 3px var(--accent); }
  100% { box-shadow: 0 0 0 0 transparent; }
}
@media (prefers-reduced-motion: reduce) {
  .entry.jump-flash .body { animation: none; }
}

.head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.event-type { font-weight: 600; font-size: 13.5px; }
.stage {
  font-size: 11px;
  color: var(--faint);
  background: var(--surface2);
  border-radius: 99px;
  padding: 1px 8px;
}
.evidence-tag {
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--accent-deep);
}

.meta { color: var(--faint); font-size: 12px; margin-top: 4px; }
.meta .seq { color: var(--muted); }
.meta .abs { white-space: nowrap; }

.payload-toggle {
  margin-top: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-deep);
}
.payload-toggle:hover { text-decoration: underline; }
.payload {
  margin-top: 8px;
  padding: 12px 14px;
  background: var(--surface2);
  border-radius: var(--radius-sm);
  font-size: 12px;
  overflow-x: auto;
  white-space: pre;
}
</style>
