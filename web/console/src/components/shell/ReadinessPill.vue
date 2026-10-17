<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// "Agent Readiness" pill in the topbar — label · progress bar · numeric
// score. Self-loading: fetches GET /catalog/readiness on mount if the
// catalog store doesn't already have it, so the topbar doesn't depend on
// any particular surface mounting first.
//
// Hidden when there's no current store (e.g. operator viewing the board)
// or when the score hasn't loaded yet.

import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCatalogStore } from '@/stores/catalog'
import { useStoresStore } from '@/stores/stores'

const stores = useStoresStore()
const catalog = useCatalogStore()
const { t } = useI18n()

async function ensureLoaded() {
  if (!stores.currentStore) return
  const stale = catalog.readiness == null || catalog.storeId !== stores.currentStoreId
  if (stale && !catalog.loading) {
    await catalog.load()
  }
}
onMounted(ensureLoaded)
watch(() => stores.currentStoreId, ensureLoaded)

const score = computed(() => catalog.readiness?.overall ?? null)
const visible = computed(() => !!stores.currentStore && score.value !== null)
</script>

<template>
  <div v-if="visible" class="readiness" :title="t('shell.readiness.title', { score })">
    <span class="label mono">{{ t('shell.readiness.label') }}</span>
    <div class="pbar" aria-hidden="true">
      <i :style="{ width: `${score}%` }" />
    </div>
    <span class="score">{{ score }}</span>
  </div>
</template>

<style scoped>
.readiness {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--muted);
  font-size: 13px;
}
.label { font-weight: 500; }
.pbar {
  width: 84px;
  height: 6px;
  border-radius: 99px;
  background: var(--surface2);
  overflow: hidden;
}
.pbar > i {
  display: block;
  height: 100%;
  background: var(--accent);
  border-radius: 99px;
  transition: width 0.6s cubic-bezier(0.3, 1, 0.4, 1);
}
.score { font-weight: 600; color: var(--ink); }
/* Narrow topbar: keep the score, drop the label and bar (the title still
   carries the full text). Measured against the topbar (AppShell), not the
   window, since the sidebar's width changes the room it has. */
@container topbar (max-width: 900px) {
  .label, .pbar { display: none; }
}
</style>
