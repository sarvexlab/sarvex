<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Products tab — readiness summary + per-product table, row → ProductDrawer.
// GET /catalog/readiness, GET /catalog/products → useCatalogStore
//
// Deep-links to a product via /store?product=<sku> — clicking a row sets the
// query and the drawer opens; closing clears it.

import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCatalogStore } from '@/stores/catalog'
import ProductDrawer from '@/components/store/ProductDrawer.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import ReadinessScore from '@/components/ui/ReadinessScore.vue'
import { dimLabel } from '@/lib/readiness'

const emit = defineEmits<{ retry: [] }>()

const route = useRoute()
const router = useRouter()
const catalog = useCatalogStore()
const { t } = useI18n()

const showSkel = computed(() => !catalog.loaded && catalog.loading)
const openSku = computed(() => (route.query.product as string | undefined) ?? null)

// Client-side only — filters whatever pages are already loaded. Searching
// doesn't fetch further pages itself; use "Load more" first to widen the set.
const skuSearch = ref('')
const filteredProducts = computed(() => {
  const q = skuSearch.value.trim().toLowerCase()
  if (!q) return catalog.products
  return catalog.products.filter((p) => p.sku?.toLowerCase().includes(q))
})

function openProduct(sku: string) {
  router.replace({ query: { ...route.query, product: sku } })
}
function closeProduct() {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { product: _drop, ...rest } = route.query
  router.replace({ query: rest })
}
</script>

<template>
  <ErrorBanner v-if="catalog.error" :message="catalog.error" @retry="emit('retry')" />
  <section v-if="showSkel" class="skel-block">
    <Skeleton height="92px" radius="11px" />
    <Skeleton height="220px" radius="11px" />
  </section>
  <section v-else>
    <div v-if="catalog.readiness" class="card readiness-card">
      <div class="readiness-score-block">
        <ReadinessScore :score="catalog.readiness.overall ?? 0" :size="88" :stroke-width="8" />
        <div class="eyebrow tight">{{ t('store.products.readinessLabel') }}</div>
      </div>
      <div class="readiness-dims">
        <div v-for="(v, k) in catalog.readiness.dimensions" :key="k" class="dim">
          <div class="dim-label">{{ dimLabel(k) }}</div>
          <div class="dim-bar"><i :style="{ width: (v ?? 0) + '%' }" /></div>
          <div class="dim-val mono">{{ v }}</div>
        </div>
      </div>
    </div>

    <p class="ready-note">{{ t('store.products.readyNote') }}</p>

    <div class="card ptable-wrap">
      <div class="search-row">
        <input
          v-model="skuSearch"
          type="text"
          class="sku-search"
          :placeholder="t('store.products.searchPlaceholder')"
          :aria-label="t('store.products.searchAriaLabel')"
        />
        <span v-if="skuSearch" class="search-count mono">
          {{ t('store.products.countOf', { filtered: filteredProducts.length, total: catalog.products.length }) }}
        </span>
      </div>
      <table class="ptable">
        <thead>
          <tr>
            <th>{{ t('store.products.columns.product') }}</th>
            <th>{{ t('store.products.columns.score') }}</th>
            <th>{{ t('store.products.columns.issues') }}</th>
            <th>{{ t('store.products.columns.agentReady') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in filteredProducts" :key="p.sku" @click="p.sku && openProduct(p.sku)">
            <td>
              <div class="pr-name">{{ p.title }}</div>
              <div class="pr-id mono">{{ p.sku }}</div>
            </td>
            <td class="mono">{{ p.score }}</td>
            <td class="mono muted">{{ p.open_issue_count ?? 0 }}</td>
            <td>
              <span v-if="!p.has_blocking_issue" class="ready">
                <span class="dot-ok" /> {{ t('store.products.ready') }}
              </span>
              <span v-else class="not-ready">
                <span class="dot-warn" />
                {{ (p.open_issue_count ?? 0) === 1
                  ? t('store.products.issueSingular', { count: p.open_issue_count ?? 0 })
                  : t('store.products.issuePlural', { count: p.open_issue_count ?? 0 }) }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="skuSearch && filteredProducts.length === 0" class="no-results">
        {{ t('store.products.noResults', { query: skuSearch }) }}
      </div>
    </div>

    <div v-if="catalog.hasMore" class="load-more">
      <button
        type="button"
        class="load-more-btn"
        :disabled="catalog.loadingMore"
        @click="catalog.loadMore()"
      >
        {{ catalog.loadingMore ? t('store.products.loading') : t('store.products.loadMore') }}
      </button>
    </div>
  </section>

  <ProductDrawer v-if="openSku" :key="openSku" :sku="openSku" @close="closeProduct" />
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.eyebrow.tight { margin: 0; }

.readiness-card {
  padding: 22px 24px;
  margin-bottom: 18px;
  display: flex;
  align-items: center;
  gap: 30px;
  flex-wrap: wrap;
}
.readiness-score-block {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.readiness-dims {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
}
.dim { display: flex; flex-direction: column; gap: 4px; }
.dim-label {
  font-size: 11.5px;
  color: var(--muted);
  text-transform: capitalize;
}
.dim-bar {
  height: 5px;
  border-radius: 99px;
  background: var(--surface2);
  overflow: hidden;
}
.dim-bar > i {
  display: block;
  height: 100%;
  background: var(--accent);
  border-radius: 99px;
}
.dim-val { font-size: 12px; color: var(--muted); align-self: flex-end; }

.ready-note { color: var(--muted); font-size: 12.5px; margin: -6px 2px 14px; }
.ptable-wrap { padding: 8px 14px 14px; overflow-x: auto; }
.search-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 0 12px;
}
.sku-search {
  flex: 0 1 280px;
  padding: 8px 12px;
  border-radius: var(--radius);
  border: 1px solid var(--line);
  background: var(--surface2);
  color: var(--ink);
  font-size: 13px;
}
.sku-search:focus {
  outline: none;
  border-color: var(--accent);
}
.search-count { color: var(--faint); font-size: 12px; }
.no-results { padding: 20px 14px; text-align: center; color: var(--muted); font-size: 13px; }
.ptable { width: 100%; border-collapse: collapse; font-size: var(--fs-small); }
.ptable th {
  text-align: left;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--faint);
  padding: 0 14px 11px;
}
.ptable td { padding: 14px; border-top: 1px solid var(--line); }
.ptable tbody tr { cursor: pointer; transition: background 0.12s; }
.ptable tbody tr:hover { background: var(--surface2); }
.pr-name { font-weight: 600; }
.pr-id { color: var(--faint); font-size: 12px; }
.muted { color: var(--muted); }
.ready, .not-ready { display: inline-flex; align-items: center; gap: 7px; }
.not-ready { color: var(--warn); }
.dot-ok, .dot-warn { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.dot-ok { background: var(--ok); }
.dot-warn { background: var(--warn); }

.skel-block { display: flex; flex-direction: column; gap: 16px; }

.load-more { display: flex; justify-content: center; margin-top: 18px; }
.load-more-btn {
  padding: 9px 20px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--line);
  box-shadow: var(--shadow);
  color: var(--ink);
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;
  transition: box-shadow 0.18s, transform 0.18s;
}
.load-more-btn:hover:not(:disabled) { box-shadow: var(--shadow-lg); transform: translateY(-1px); }
.load-more-btn:disabled { opacity: 0.6; cursor: default; }
</style>
