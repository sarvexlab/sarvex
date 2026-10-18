<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Per-merchant across-stores summary. The ONLY surface above the store scope.
// With a single store this never renders — the router lands you straight on
// /path. Shown when stores.stores.length > 1.
//
// Each row links to that store's Path; account-level shortcuts (Settings,
// Billing) live in the header bar.

import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useAccountStore } from '@/stores/account'
import { useConnectorsStore } from '@/stores/connectors'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const router = useRouter()
const { t } = useI18n()
const stores = useStoresStore()
const account = useAccountStore()
const connectors = useConnectorsStore()

async function refresh() {
  await Promise.all([
    stores.loaded ? Promise.resolve() : stores.load(),
    account.load(),
    connectors.connectors.length ? Promise.resolve() : connectors.load(),
  ])
}
onMounted(refresh)

const showSkeleton = computed(() => !stores.loaded && stores.loading)

// Progress through the store's own activation lifecycle. 100% is `active`
// — the store's endpoints live and monitored — not "approved by a
// distribution partner", which is what the journey-keyed version meant and
// which a merchant might never reach by choice.
const STAGE_PCT: Record<string, number> = {
  draft: 10,
  connector_linked: 35,
  profile_published: 60,
  verified: 85,
  active: 100,
  // Suspended keeps the ground the store covered rather than reading as
  // no progress at all.
  suspended: 85,
}

const merchantName = computed(() => account.account?.name ?? t('accountHome.accountFallback'))

function openStore(storeId: string) {
  stores.select(storeId)
  router.push('/path')
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">{{ merchantName }} · {{ t('accountHome.eyebrowSuffix') }}</div>
      <h1 class="page-title display">{{ t('accountHome.title') }}</h1>
      <p class="page-sub">
        {{ t('accountHome.sub', { count: stores.stores.length }) }}
      </p>
    </div>

    <ErrorBanner v-if="account.error" :message="account.error" @retry="refresh" />

    <div v-if="showSkeleton" class="list" aria-hidden="true">
      <div v-for="i in 3" :key="i" class="card store-card">
        <Skeleton width="38px" height="38px" radius="9px" />
        <div class="main skel-stack">
          <Skeleton width="35%" height="15px" />
          <Skeleton width="50%" height="12px" />
          <Skeleton width="280px" height="5px" radius="99px" />
        </div>
      </div>
    </div>

    <div v-else class="list">
      <button
        v-for="s in stores.stores"
        :key="s.store_id"
        type="button"
        class="card store-card"
        @click="openStore(s.store_id ?? '')"
      >
        <div class="lead">
          <span class="dot"><Icon name="store" /></span>
        </div>
        <div class="main">
          <div class="name">{{ s.name }}</div>
          <div class="domain mono">{{ s.domain }}</div>
          <div class="track">
            <i
              :style="{
                width: (STAGE_PCT[s.ucp_state ?? 'draft'] ?? 0) + '%',
                background: s.ucp_state === 'active' ? 'var(--ok)' : 'var(--accent)',
              }"
            />
          </div>
        </div>
        <div class="meta">
          <Chip :kind="s.ucp_state === 'active' ? 'ok' : 'you'">
            {{ t(`journey.stage.${s.ucp_state ?? 'draft'}`) }}
          </Chip>
          <span class="connector-state mono">
            {{ connectors.forStore(s.store_id ?? '')?.status ?? '—' }}
          </span>
        </div>
        <span class="chev"><Icon name="chevR" /></span>
      </button>
    </div>

    <p class="foot">
      {{ t('accountHome.footer') }}
    </p>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 24px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.list { display: flex; flex-direction: column; gap: var(--gap-feed); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.store-card {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: var(--pad-card);
  text-align: left;
  cursor: pointer;
  transition: box-shadow 0.18s, transform 0.18s;
  border: 0;
  width: 100%;
  font: inherit;
  color: inherit;
}
.store-card:hover {
  box-shadow: var(--shadow-lg);
  transform: translateY(-1px);
}

.lead { flex-shrink: 0; margin-top: 1px; }
.dot {
  width: 38px;
  height: 38px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: var(--surface2);
  color: var(--muted);
}
.dot :deep(svg) { width: 18px; height: 18px; }

.main { flex: 1; min-width: 0; }
.skel-stack { display: flex; flex-direction: column; gap: 8px; padding-top: 2px; }
.name { font-weight: 600; font-size: calc(var(--fs-body) + 1px); }
.domain { font-size: 12.5px; color: var(--faint); margin-top: 2px; }
.track {
  height: 5px;
  border-radius: 99px;
  background: var(--surface2);
  overflow: hidden;
  margin-top: 11px;
  max-width: 280px;
}
.track > i {
  display: block;
  height: 100%;
  border-radius: 99px;
  transition: width 0.6s cubic-bezier(0.3, 1, 0.4, 1);
}

.meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
  flex-shrink: 0;
  text-align: right;
}
.connector-state { color: var(--faint); font-size: 12px; }

.chev { color: var(--faint); align-self: center; }
.chev :deep(svg) { width: 16px; height: 16px; }

.foot {
  font-size: 12.5px;
  color: var(--faint);
  margin-top: 16px;
  text-align: center;
  line-height: 1.6;
}
</style>
