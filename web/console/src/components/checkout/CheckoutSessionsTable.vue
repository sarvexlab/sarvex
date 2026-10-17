<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Shared table for both Live and History modes (R1/R3). History adds a
// terminal_at column. Row click deep-links to the session detail page
// (R6) — the checkout_id is the shareable identifier (R11).
//
// Responsive: horizontal scroll on narrow viewports, same convention as
// StoreProductsPanel's .ptable-wrap — this codebase has no breakpoint-based
// card-collapse pattern for tables to reuse instead (R15).

import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { formatMinorUnits } from '@/lib/money'
import { formatRelativeTime, formatUTCTimestamp } from '@/lib/datetime'
import { checkoutStateChipKind, causeLabel } from '@/lib/checkoutSession'
import Chip from '@/components/ui/Chip.vue'
import type { CheckoutSession } from '@/api/checkoutSessions'

defineProps<{ sessions: CheckoutSession[]; mode: 'live' | 'history' }>()

const router = useRouter()
const { t } = useI18n()

function openSession(checkoutId: string | undefined) {
  if (!checkoutId) return
  router.push(`/checkout-sessions/${encodeURIComponent(checkoutId)}`)
}
</script>

<template>
  <div class="card ptable-wrap">
    <table class="ptable">
      <thead>
        <tr>
          <th>{{ t('checkout.table.columns.state') }}</th>
          <th>{{ t('checkout.table.columns.surface') }}</th>
          <th>{{ t('checkout.table.columns.started') }}</th>
          <th>{{ t('checkout.table.columns.lastActivity') }}</th>
          <th>{{ t('checkout.table.columns.cartTotal') }}</th>
          <th>{{ t('checkout.table.columns.stage') }}</th>
          <th>{{ t('checkout.table.columns.cause') }}</th>
          <th v-if="mode === 'history'">{{ t('checkout.table.columns.terminalAt') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="s in sessions"
          :key="s.checkout_id"
          tabindex="0"
          role="button"
          :aria-label="t('checkout.table.openAriaLabel', { id: s.checkout_id })"
          @click="openSession(s.checkout_id)"
          @keydown.enter="openSession(s.checkout_id)"
        >
          <td>
            <Chip :kind="checkoutStateChipKind(s.state)">{{ s.state ?? t('checkout.table.unknown') }}</Chip>
          </td>
          <td class="mono muted">{{ s.surface ?? '—' }}</td>
          <td :title="formatUTCTimestamp(s.started_at)">{{ formatRelativeTime(s.started_at) }}</td>
          <td :title="formatUTCTimestamp(s.last_activity_at)">
            {{ formatRelativeTime(s.last_activity_at) }}
          </td>
          <td class="mono">
            {{ formatMinorUnits(s.cart_summary?.total_minor ?? 0, s.cart_summary?.currency) }}
          </td>
          <td class="mono muted">{{ s.current_stage ?? '—' }}</td>
          <td>
            <span v-if="s.diagnosis?.cause" class="cause">{{ causeLabel(t, s.diagnosis.cause) }}</span>
            <span v-else class="muted">—</span>
          </td>
          <td v-if="mode === 'history'" :title="formatUTCTimestamp(s.terminal_at)">
            {{ s.terminal_at ? formatRelativeTime(s.terminal_at) : '—' }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.ptable-wrap { padding: 6px 10px 10px; overflow-x: auto; }
.ptable { width: 100%; border-collapse: collapse; font-size: var(--fs-small); white-space: nowrap; }
.ptable th {
  text-align: left;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--faint);
  padding: 0 14px 11px;
}
.ptable td { padding: 13px 14px; border-top: 1px solid var(--line); }
.ptable tbody tr { cursor: pointer; transition: background 0.12s; }
.ptable tbody tr:hover,
.ptable tbody tr:focus-visible { background: var(--surface2); outline: none; }
.muted { color: var(--faint); }
.cause { color: var(--warn); }
</style>
