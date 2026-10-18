<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Product demand (R12). Counts only: GET /insights/product_demand has no
// reached_cart_count/completed_count (no telemetry correlates a catalog
// request to a later checkout — see api/insights.ts's top-of-file note), so
// there's nothing to derive a rate from. request_count + surface_breakdown
// are shown; countsOnlyNote explains the omission rather than leaving it
// unexplained.
//
// "Paginated" per the task brief, but GET /insights/product_demand's
// pagination is a fixed top-N (`limit`, default 100) with page.has_more
// always false server-side (internal/planes/control/api/insights.go's
// getProductDemand) — there is no cursor to page through. limitOptions
// below is a top-N selector, not cursor-based "load more".

import { useI18n } from 'vue-i18n'
import { surfaceLabel } from '@/lib/insights'
import type { ProductDemand } from '@/api/insights'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import Field from '@/components/ui/Field.vue'

defineProps<{
  rows: ProductDemand[]
  loading: boolean
  error: string | null
  limit: number
}>()
const emit = defineEmits<{ retry: []; 'update:limit': [number] }>()

const { t } = useI18n()

const LIMIT_OPTIONS = [25, 50, 100]

function surfaceEntries(row: ProductDemand): Array<[string, number]> {
  return Object.entries(row.surface_breakdown ?? {}).sort((a, b) => b[1] - a[1])
}
</script>

<template>
  <section class="section">
    <div class="section-head">
      <div>
        <h2 class="section-title display">{{ t('insights.productDemand.title') }}</h2>
        <p class="section-sub">{{ t('insights.productDemand.subtitle') }}</p>
        <p class="scope-note">{{ t('analytics.merchantWideNote') }}</p>
      </div>
      <Field :label="t('insights.productDemand.limitLabel')" class="limit-field">
        <select :value="limit" @change="emit('update:limit', Number(($event.target as HTMLSelectElement).value))">
          <option v-for="n in LIMIT_OPTIONS" :key="n" :value="n">{{ t('insights.productDemand.topN', { n }) }}</option>
        </select>
      </Field>
    </div>

    <ErrorBanner v-if="error" :message="error" @retry="emit('retry')" />

    <div v-if="loading" class="skel-block">
      <Skeleton height="180px" radius="var(--radius)" />
    </div>

    <template v-else-if="!error">
      <div v-if="rows.length === 0" class="card empty-note">
        <div class="big">{{ t('insights.productDemand.empty.title') }}</div>
        <p>{{ t('insights.productDemand.empty.body') }}</p>
      </div>
      <template v-else>
        <p class="suppression-note">{{ t('insights.suppressionNote') }} {{ t('insights.productDemand.countsOnlyNote') }}</p>
        <div class="card ptable-wrap">
          <table class="ptable">
            <thead>
              <tr>
                <th class="col-product">{{ t('insights.productDemand.columns.product') }}</th>
                <th class="col-count">{{ t('insights.productDemand.columns.requestCount') }}</th>
                <th class="col-surfaces">{{ t('insights.productDemand.columns.surfaces') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.product_id">
                <td class="col-product mono">{{ row.product_id }}</td>
                <td class="col-count mono">{{ row.request_count }}</td>
                <td class="col-surfaces">
                  <div class="surface-chips">
                    <span v-for="[surface, count] in surfaceEntries(row)" :key="surface" class="surface-chip">
                      {{ surfaceLabel(t, surface) }}: <span class="mono">{{ count }}</span>
                    </span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.section { display: flex; flex-direction: column; gap: 12px; }
.section-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.section-title { font-size: 22px; }
.section-sub { color: var(--muted); font-size: 14px; margin-top: 6px; max-width: 68ch; }
.scope-note { color: var(--faint); font-size: 12px; margin-top: 4px; }
.limit-field { margin-bottom: 0; min-width: 120px; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.suppression-note { font-size: 12px; color: var(--faint); margin: 0; }
.skel-block { display: flex; flex-direction: column; gap: 12px; }

.empty-note { text-align: center; padding: 44px 20px; color: var(--muted); }
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 18px;
  color: var(--ink);
  margin-bottom: 6px;
}
.empty-note p { max-width: 52ch; margin: 0 auto; font-size: 13.5px; }

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
.ptable td { padding: 13px 14px; border-top: 1px solid var(--line); }
.col-product { word-break: break-all; }
.col-count { white-space: nowrap; }
.surface-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.surface-chip { font-size: 11.5px; background: var(--surface2); color: var(--muted); padding: 3px 9px; border-radius: 99px; white-space: nowrap; }
</style>
