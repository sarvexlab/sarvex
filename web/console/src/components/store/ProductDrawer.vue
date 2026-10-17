<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Product drill-in — right-side slide-in drawer over the Store surface.
//
// The design has this as a drawer, not a route. Same drawer chrome as
// TaskDrawer / SettingsDrawer. Reachable via /store?product=<sku> so links
// to a specific product are still shareable.
//
// Body: Issues (top) + Attribute mappings (below). Both sections come from
// GET /catalog/products/{sku}.

import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCatalogStore } from '@/stores/catalog'
import { problemLabel } from '@/lib/issue'
import { dimLabel } from '@/lib/readiness'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const props = defineProps<{ sku: string }>()
const emit = defineEmits<{ close: [] }>()

const catalog = useCatalogStore()
const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const detail = computed(() => catalog.detail)
const loading = computed(() => catalog.detailLoading)

// The resolve stage emits one entry per catalogue target attribute, so a
// product with few attributes is mostly `unresolved` rows. Those are worth
// showing — they are what the merchant is missing — but they should not bury
// the mappings that did resolve.
const TIER_ORDER: Record<string, number> = { rule: 0, learned: 1, unresolved: 2 }
const tierChip: Record<string, 'ok' | 'quiet' | 'warn'> = {
  rule: 'ok',
  learned: 'quiet',
  unresolved: 'warn',
}

const sortedMappings = computed(() =>
  [...(detail.value?.attribute_mappings ?? [])].sort(
    (a, b) =>
      (TIER_ORDER[a.tier ?? 'unresolved'] ?? 3) - (TIER_ORDER[b.tier ?? 'unresolved'] ?? 3) ||
      (a.catalog_attribute ?? '').localeCompare(b.catalog_attribute ?? ''),
  ),
)

// The product's score is built from these areas: each is the share of that
// area's checks the product passes, and the overall score combines them.
const breakdown = computed(() =>
  Object.entries(detail.value?.dimensions ?? {}).map(([key, v]) => ({
    key,
    label: dimLabel(key),
    value: v ?? 0,
  })),
)

async function load() {
  await catalog.loadDetail(props.sku)
}
onMounted(load)
watch(() => props.sku, load)

function onClose() {
  // Drop the ?product= query so closing is bidirectional.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { product: _drop, ...rest } = route.query
  router.replace({ query: rest })
  emit('close')
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') onClose()
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="scrim" @click="onClose" />
  <aside class="drawer" role="dialog" aria-modal="true">
    <ErrorBanner v-if="catalog.detailError" :message="catalog.detailError" @retry="load" />

    <template v-if="loading">
      <div class="drawer-head">
        <div class="head-top">
          <Skeleton width="80px" height="22px" radius="99px" />
          <Skeleton width="33px" height="33px" radius="50%" />
        </div>
        <Skeleton width="70%" height="26px" />
        <Skeleton width="50%" height="14px" />
      </div>
      <div class="drawer-body">
        <Skeleton height="180px" radius="11px" />
      </div>
    </template>

    <template v-else-if="detail">
      <div class="drawer-head">
        <div class="head-top">
          <Chip kind="quiet">{{ detail.sku }}</Chip>
          <button class="icon-btn" :aria-label="t('store.drawer.close')" @click="onClose"><Icon name="x" /></button>
        </div>
        <h2 class="display title">{{ detail.title }}</h2>
        <div class="meta">
          <span class="mono">{{ t('store.drawer.scoreOf100', { score: detail.score }) }}</span>
          <span>{{ detail.open_issue_count === 1
            ? t('store.drawer.openIssueSingular', { count: detail.open_issue_count })
            : t('store.drawer.openIssuePlural', { count: detail.open_issue_count }) }}</span>
          <span v-if="detail.has_blocking_issue" class="warn">{{ t('store.drawer.blocksCheckout') }}</span>
        </div>
      </div>

      <div class="drawer-body">
        <div v-if="breakdown.length > 0" class="section">
          <div class="eyebrow">{{ t('store.drawer.breakdownTitle') }}</div>
          <p class="mapping-note">{{ t('store.drawer.breakdownNote') }}</p>
          <div class="card breakdown">
            <div v-for="d in breakdown" :key="d.key" class="dim">
              <div class="dim-label">{{ d.label }}</div>
              <div class="dim-bar"><i :style="{ width: d.value + '%' }" /></div>
              <div class="dim-val mono">{{ d.value }}</div>
            </div>
          </div>
        </div>

        <div v-if="detail.issues && detail.issues.length > 0" class="section">
          <div class="eyebrow">{{ t('store.drawer.openIssuesTitle') }}</div>
          <div class="card list">
            <div
              v-for="(iss, i) in detail.issues"
              :key="iss.issue_id"
              class="row"
              :class="{ first: i === 0 }"
            >
              <span class="dot" :class="iss.blocks_checkout ? 'dot-warn' : 'dot-quiet'" />
              <div class="row-main">
                <div class="row-title">
                  <span class="mono">{{ iss.field }}</span>
                  <span class="row-sep">·</span>
                  <span>{{ problemLabel(t, iss.problem) }}</span>
                </div>
              </div>
              <Chip v-if="iss.blocks_checkout" kind="danger">{{ t('store.drawer.blocks') }}</Chip>
              <Chip v-else kind="quiet">{{ t('store.drawer.quality') }}</Chip>
            </div>
          </div>
        </div>

        <div class="section">
          <div class="eyebrow">{{ t('store.drawer.mappingTitle') }}</div>
          <p class="mapping-note">{{ t('store.drawer.mappingNote') }}</p>
          <div
            v-if="!detail.attribute_mappings || detail.attribute_mappings.length === 0"
            class="empty"
          >
            {{ t('store.drawer.noMappings') }}
          </div>
          <div v-else class="card list">
            <div
              v-for="(m, i) in sortedMappings"
              :key="i"
              class="row"
              :class="{ first: i === 0 }"
            >
              <div class="row-main">
                <div class="row-title">
                  <span v-if="m.source_attribute">{{ m.source_attribute }}</span>
                  <span v-else class="unmapped">{{ t('store.drawer.unresolvedSource') }}</span>
                  <span class="arrow"><Icon name="arrowR" /></span>
                  <span class="mono">{{ m.catalog_attribute }}</span>
                </div>
                <!-- Confidence is the resolving rule's confidence. An
                     unresolved entry has no resolving rule, so its 0 is the
                     absence of a number rather than a measured one. -->
                <div v-if="m.tier !== 'unresolved' && m.confidence != null" class="row-sub">
                  {{ t('store.drawer.confidence') }} <span class="mono">{{ (m.confidence * 100).toFixed(0) }}%</span>
                </div>
              </div>
              <Chip :kind="tierChip[m.tier ?? 'unresolved'] ?? 'quiet'">
                {{ t(`store.drawer.${m.tier ?? 'unresolved'}`) }}
              </Chip>
            </div>
          </div>
        </div>
      </div>
    </template>
  </aside>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  background: color-mix(in oklab, var(--ink) 32%, transparent);
  backdrop-filter: blur(2px);
  z-index: 60;
  animation: fade 0.2s both;
}
@keyframes fade { from { opacity: 0; } to { opacity: 1; } }

.drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(540px, 96vw);
  background: var(--bg);
  z-index: 61;
  box-shadow: -20px 0 60px -20px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  animation: slidein 0.32s cubic-bezier(0.22, 0.9, 0.3, 1) both;
}
@keyframes slidein { from { transform: translateX(100%); } to { transform: none; } }

.drawer-head {
  padding: 22px 26px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.head-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
}
.title { font-size: 23px; }
.meta {
  display: flex;
  gap: 16px;
  margin-top: 10px;
  color: var(--muted);
  font-size: 13.5px;
  flex-wrap: wrap;
}
.meta .warn { color: var(--warn); font-weight: 600; }

.icon-btn {
  width: 33px;
  height: 33px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--muted);
  background: none;
  border: 0;
  cursor: pointer;
}
.icon-btn:hover { background: var(--surface2); color: var(--ink); }

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 24px 26px 40px;
  display: flex;
  flex-direction: column;
  gap: 22px;
}

.section .eyebrow {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--faint);
  margin-bottom: 10px;
}

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.list { overflow: hidden; }
.empty { padding: 18px; color: var(--muted); font-size: 13.5px; }
.breakdown { padding: 16px 18px; display: flex; flex-direction: column; gap: 12px; }
.dim { display: grid; grid-template-columns: 140px 1fr 36px; align-items: center; gap: 12px; }
.dim-label { font-size: 13px; color: var(--ink); }
.dim-bar { height: 5px; border-radius: 99px; background: var(--surface2); overflow: hidden; }
.dim-bar > i { display: block; height: 100%; background: var(--accent); border-radius: 99px; }
.dim-val { font-size: 12px; color: var(--muted); text-align: right; }
.mapping-note { color: var(--muted); font-size: 12.5px; margin: -2px 0 8px; }
/* A placeholder, not a value — smaller and lighter than the attribute name
   it sits beside, so the row reads as "→ age_group" first. */
.unmapped { color: var(--faint); font-style: italic; font-size: 12px; font-weight: 400; }

.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }

.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-warn { background: var(--warn); }
.dot-quiet { background: var(--line2); }

.row-main { flex: 1; min-width: 0; }
.row-title {
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.row-sep { color: var(--faint); font-weight: 400; }
.arrow { color: var(--accent); display: inline-flex; }
.arrow :deep(svg) { width: 14px; height: 14px; }
.row-sub { font-size: 12.5px; color: var(--muted); margin-top: 5px; }
</style>
