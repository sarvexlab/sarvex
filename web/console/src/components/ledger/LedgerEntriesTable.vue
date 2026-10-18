<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Entries table (R9-R15). Server order is rendered as-is — no client sort.
// Lost revenue and recommended_action are first-class columns that survive
// the responsive collapse (R19); surface/store/stage fade first.
//
// Row expand (R13) shows checkout_id/failure_code/currency/status_changed_at
// and deep-links to the Checkout Escalation Console's session detail route
// when it exists — it does in this build ('checkout-session-detail'), so
// this always renders a link, never a dead one; the router.hasRoute check
// keeps that true even if the route is ever removed.

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { formatMinorUnits } from '@/lib/money'
import { formatRelativeTime, formatUTCTimestamp } from '@/lib/datetime'
import { causeLabel, forwardStatuses, statusChipKind, causeChipKind } from '@/lib/ledger'
import type { LedgerEntry, LedgerEntryStatus } from '@/api/ledger'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'

const props = defineProps<{
  entries: LedgerEntry[]
  statusPending: Record<string, boolean>
}>()
const emit = defineEmits<{
  'change-status': [entryId: string, status: LedgerEntryStatus]
  'bulk-change-status': [entryIds: string[], status: LedgerEntryStatus]
}>()

const { t } = useI18n()
const router = useRouter()
const hasSessionRoute = router.hasRoute('checkout-session-detail')

const expandedId = ref<string | null>(null)
function toggleExpand(id: string | undefined) {
  if (!id) return
  expandedId.value = expandedId.value === id ? null : id
}

const selected = ref<Set<string>>(new Set())
function toggleSelect(id: string | undefined) {
  if (!id) return
  const next = new Set(selected.value)
  next.has(id) ? next.delete(id) : next.add(id)
  selected.value = next
}
const allSelected = computed(
  () => props.entries.length > 0 && props.entries.every((e) => e.entry_id && selected.value.has(e.entry_id)),
)
function toggleSelectAll() {
  if (allSelected.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(props.entries.map((e) => e.entry_id).filter((x): x is string => !!x))
  }
}

function bulkStatus(status: LedgerEntryStatus) {
  emit('bulk-change-status', [...selected.value], status)
  selected.value = new Set()
}

function sessionRoute(checkoutId: string | undefined) {
  if (!checkoutId || !hasSessionRoute) return null
  return { name: 'checkout-session-detail', params: { checkoutId } }
}
</script>

<template>
  <div>
    <div v-if="selected.size > 0" class="bulk-bar">
      <span>{{ t('ledger.entries.bulk.selectedCount', { count: selected.size }) }}</span>
      <button type="button" class="bulk-btn" @click="bulkStatus('acknowledged')">
        {{ t('ledger.entries.bulk.acknowledge') }}
      </button>
      <button type="button" class="bulk-btn" @click="bulkStatus('resolved')">
        {{ t('ledger.entries.bulk.resolve') }}
      </button>
    </div>

    <div class="card ptable-wrap">
      <table class="ptable">
        <thead>
          <tr>
            <th class="col-select">
              <input type="checkbox" :checked="allSelected" :aria-label="t('ledger.entries.selectAll')" @change="toggleSelectAll" />
            </th>
            <th class="col-revenue">{{ t('ledger.entries.columns.lostRevenue') }}</th>
            <th class="col-cause">{{ t('ledger.entries.columns.cause') }}</th>
            <th class="col-occurred">{{ t('ledger.entries.columns.occurredAt') }}</th>
            <th class="col-stage">{{ t('ledger.entries.columns.stage') }}</th>
            <th class="col-surface">{{ t('ledger.entries.columns.surface') }}</th>
            <th class="col-status">{{ t('ledger.entries.columns.status') }}</th>
            <th class="col-action">{{ t('ledger.entries.columns.recommendedAction') }}</th>
            <th class="col-expand" />
          </tr>
        </thead>
        <tbody>
          <template v-for="e in entries" :key="e.entry_id">
            <tr>
              <td class="col-select">
                <input
                  type="checkbox"
                  :checked="!!e.entry_id && selected.has(e.entry_id)"
                  :aria-label="t('ledger.entries.selectEntry', { id: e.entry_id })"
                  @change="toggleSelect(e.entry_id)"
                />
              </td>
              <td class="col-revenue mono">
                <span v-if="e.revenue_unknown" class="not-priced">{{ t('ledger.entries.notPriced') }}</span>
                <span v-else>{{ formatMinorUnits(e.lost_revenue_minor ?? 0, e.currency ?? undefined) }}</span>
              </td>
              <td class="col-cause"><Chip :kind="causeChipKind(e.cause)">{{ causeLabel(t, e.cause) }}</Chip></td>
              <td class="col-occurred" :title="formatUTCTimestamp(e.occurred_at)">
                {{ formatRelativeTime(e.occurred_at) }}
              </td>
              <td class="col-stage muted">{{ e.stage ?? '—' }}</td>
              <td class="col-surface muted">{{ e.surface ?? '—' }}</td>
              <td class="col-status">
                <div class="status-cell">
                  <Chip :kind="statusChipKind(e.status)">{{ t(`ledger.entries.status.${e.status}`) }}</Chip>
                  <button
                    v-for="next in e.status ? forwardStatuses(e.status) : []"
                    :key="next"
                    type="button"
                    class="status-btn"
                    :disabled="!!(e.entry_id && statusPending[e.entry_id])"
                    @click="e.entry_id && emit('change-status', e.entry_id, next)"
                  >
                    {{ t(next === 'acknowledged' ? 'ledger.entries.acknowledge' : 'ledger.entries.resolve') }}
                  </button>
                </div>
              </td>
              <td class="col-action">{{ e.recommended_action }}</td>
              <td class="col-expand">
                <button
                  type="button"
                  class="expand-btn"
                  :aria-label="expandedId === e.entry_id ? t('ledger.entries.collapseRow') : t('ledger.entries.expandRow')"
                  @click="toggleExpand(e.entry_id)"
                >
                  <Icon :name="expandedId === e.entry_id ? 'chevD' : 'chevR'" />
                </button>
              </td>
            </tr>
            <tr v-if="expandedId === e.entry_id" class="expand-row">
              <td colspan="9">
                <div class="expand-grid">
                  <div>
                    <div class="expand-label">{{ t('ledger.entries.expand.checkoutId') }}</div>
                    <router-link v-if="sessionRoute(e.checkout_id)" :to="sessionRoute(e.checkout_id)!" class="mono link">
                      {{ e.checkout_id }}
                    </router-link>
                    <span v-else class="mono">{{ e.checkout_id ?? '—' }}</span>
                  </div>
                  <div>
                    <div class="expand-label">{{ t('ledger.entries.expand.failureCode') }}</div>
                    <span class="mono">{{ e.failure_code ?? '—' }}</span>
                  </div>
                  <div>
                    <div class="expand-label">{{ t('ledger.entries.expand.currency') }}</div>
                    <span class="mono">{{ e.currency ?? '—' }}</span>
                  </div>
                  <div>
                    <div class="expand-label">{{ t('ledger.entries.expand.statusChangedAt') }}</div>
                    <span :title="formatUTCTimestamp(e.status_changed_at)">
                      {{ formatRelativeTime(e.status_changed_at) }}
                    </span>
                  </div>
                  <div v-if="sessionRoute(e.checkout_id)">
                    <router-link :to="sessionRoute(e.checkout_id)!" class="link">
                      {{ t('ledger.entries.expand.viewSession') }}
                    </router-link>
                  </div>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
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
.ptable-wrap { padding: 6px 10px 10px; overflow-x: auto; }
.ptable { width: 100%; border-collapse: collapse; font-size: var(--fs-small); }
.ptable th {
  text-align: left;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--faint);
  padding: 0 14px 11px;
  white-space: nowrap;
}
.ptable td { padding: 13px 14px; border-top: 1px solid var(--line); white-space: nowrap; }
.col-action { white-space: normal; min-width: 220px; }
.muted { color: var(--faint); }
.not-priced { color: var(--faint); font-style: italic; }

.col-select { width: 32px; }
.status-cell { display: flex; align-items: center; gap: 8px; }
.status-btn {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--accent-deep);
  padding: 3px 9px;
  border-radius: 99px;
  box-shadow: inset 0 0 0 1px var(--accent-line);
}
.status-btn:hover:not(:disabled) { background: var(--accent-soft); }
.status-btn:disabled { opacity: 0.5; cursor: default; }

.expand-btn { color: var(--faint); padding: 4px; }
.expand-btn:hover { color: var(--ink); }
.expand-btn :deep(svg) { width: 14px; height: 14px; }

.expand-row td { padding: 0 14px 16px; border-top: none; }
.expand-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 14px;
  background: var(--surface2);
  border-radius: var(--radius-sm);
  padding: 14px 18px;
}
.expand-label { font-size: 11px; color: var(--faint); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 4px; }
.link { color: var(--accent-deep); font-weight: 600; }
.link:hover { text-decoration: underline; }

.bulk-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--accent-soft);
  color: var(--accent-deep);
  border-radius: var(--radius-sm);
  padding: 10px 16px;
  margin-bottom: 12px;
  font-size: 13px;
  font-weight: 600;
}
.bulk-btn {
  background: var(--surface);
  padding: 6px 13px;
  border-radius: 99px;
  box-shadow: inset 0 0 0 1px var(--accent-line);
}
.bulk-btn:hover { background: var(--accent-softer); }

/* R19: lost revenue + recommended action must survive the tablet collapse;
   surface/stage/occurred fade first as the least decision-relevant columns. */
@media (max-width: 900px) {
  .col-surface { display: none; }
}
@media (max-width: 760px) {
  .col-stage { display: none; }
}
@media (max-width: 620px) {
  .col-occurred { display: none; }
}
</style>
