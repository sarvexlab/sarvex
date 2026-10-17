<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Design ref: "Sarvex Console (standalone)-latest.html"'s StoreSwitcher.
// A single store (still the common V1 case) stays a static label — no
// picker chrome for something with nothing to pick between. More than one
// opens a popover: an "All stores" link back to the account home, then
// each store's name, domain, journey stage, and a status badge (a red
// "Blocked" chip if it has a checkout-blocking task, an "N for you" open-
// task count, or a checkmark when it's fully clear).

import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { useAccountStore } from '@/stores/account'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'

const router = useRouter()
const route = useRoute()
const stores = useStoresStore()
const account = useAccountStore()
const { t } = useI18n()

// The switcher shows each store's own activation state. It used to show
// journey_state, whose later values were Google's verdicts — so a store
// that was fully working but not enrolled with Google read as unfinished.
function stageLabel(stage: string): string {
  return t(`journey.stage.${stage}`)
}

const current = computed(() => stores.currentStore)
const hasMultiple = computed(() => stores.stores.length > 1)
const atHome = computed(() => route.name === 'account-home')

const open = ref(false)
function toggle() {
  open.value = !open.value
}
function close() {
  open.value = false
}

function pick(storeId: string) {
  stores.select(storeId)
  if (atHome.value) router.push({ name: 'path' })
  close()
}

function goAccountHome() {
  router.push({ name: 'account-home' })
  close()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}
watch(open, (isOpen) => {
  if (isOpen) window.addEventListener('keydown', onKey)
  else window.removeEventListener('keydown', onKey)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="current && !hasMultiple" class="store-switch static" :title="t('shell.storeSwitcher.oneStoreTitle')">
    <span class="ss-ic"><Icon name="store" /></span>
    <span class="ss-name">{{ current.name }}</span>
  </div>

  <div v-else-if="current" class="store-switch-wrap">
    <button
      type="button"
      :class="['store-switch', { open }]"
      aria-haspopup="true"
      :aria-expanded="open"
      @click="toggle"
    >
      <span class="ss-ic"><Icon name="store" /></span>
      <span class="ss-name">{{ atHome ? t('shell.storeSwitcher.allStores') : current.name }}</span>
      <span class="ss-chev"><Icon name="chevD" /></span>
    </button>

    <template v-if="open">
      <div class="ss-backdrop" @click="close" />
      <div class="ss-menu">
        <div class="ss-acct">
          <span class="eyebrow">{{ account.account?.name ?? t('shell.storeSwitcher.accountFallback') }}</span>
          <span class="ss-acct-n">{{ t('shell.storeSwitcher.storesCount', { count: stores.stores.length }) }}</span>
        </div>
        <button type="button" :class="['ss-item', 'ss-home', { sel: atHome }]" @click="goAccountHome">
          <span class="ss-home-ic"><Icon name="flag" /></span>
          <span class="ss-home-label">{{ t('shell.storeSwitcher.allStores') }}</span>
          <span class="ss-chev"><Icon name="chevR" /></span>
        </button>
        <div class="ss-div" />
        <button
          v-for="s in stores.stores"
          :key="s.store_id"
          type="button"
          :class="[
            'ss-item',
            'ss-store',
            { sel: s.store_id === current.store_id && !atHome, blocked: s.blocked },
          ]"
          @click="pick(s.store_id ?? '')"
        >
          <span class="ss-store-main">
            <span class="ss-store-name">{{ s.name }}</span>
            <span class="ss-store-sub">{{ s.domain }} · {{ stageLabel(s.ucp_state ?? 'draft') }}</span>
          </span>
          <span class="ss-store-tasks">
            <Chip v-if="s.blocked" kind="danger">{{ t('shell.storeSwitcher.blocked') }}</Chip>
            <span v-else-if="(s.open_task_count ?? 0) > 0" class="ss-count">{{ t('shell.storeSwitcher.tasksForYou', { count: s.open_task_count }) }}</span>
            <span v-else class="ss-count ok"><Icon name="check" /></span>
          </span>
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.store-switch-wrap { position: relative; }
.store-switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 11px;
  border-radius: 99px;
  max-width: 240px;
  color: var(--ink);
}
.store-switch .ss-ic { color: var(--accent-deep); display: inline-flex; }
.store-switch .ss-ic :deep(svg) { width: 15px; height: 15px; }
.store-switch .ss-name {
  font-weight: 600;
  font-size: 14px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.store-switch .ss-chev { color: var(--faint); display: inline-flex; transition: transform 0.18s; }
.store-switch .ss-chev :deep(svg) { width: 14px; height: 14px; }
.store-switch.static { padding-left: 12px; border-left: 1px solid var(--line); border-radius: 0; cursor: default; }
button.store-switch { box-shadow: inset 0 0 0 1px var(--line2); transition: box-shadow 0.15s, background 0.15s; }
button.store-switch:hover { background: var(--surface2); }
button.store-switch.open { box-shadow: inset 0 0 0 1px var(--accent-line); background: var(--accent-softer); }
button.store-switch.open .ss-chev { transform: rotate(180deg); }

.ss-backdrop { position: fixed; inset: 0; z-index: 48; }
.ss-menu {
  position: absolute;
  top: calc(100% + 8px);
  left: 0;
  z-index: 49;
  width: 320px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg);
  padding: 7px;
}
.ss-acct { display: flex; align-items: center; justify-content: space-between; padding: 8px 11px 9px; }
.ss-acct .eyebrow { margin: 0; }
.ss-acct-n { font-size: 12px; color: var(--faint); }

.ss-item {
  display: flex;
  align-items: center;
  gap: 11px;
  width: 100%;
  text-align: left;
  padding: 10px 11px;
  border-radius: var(--radius-sm);
  transition: background 0.13s;
}
.ss-item:hover { background: var(--surface2); }
.ss-item.sel { background: var(--accent-softer); }
.ss-item .ss-chev :deep(svg), .ss-home-ic :deep(svg) { width: 14px; height: 14px; }

.ss-home { color: var(--muted); font-weight: 600; font-size: 13.5px; }
.ss-home-ic { color: var(--faint); display: inline-flex; }
.ss-home-label { flex: 1; }

.ss-div { height: 1px; background: var(--line); margin: 5px 4px; }

.ss-store-main { flex: 1; min-width: 0; }
.ss-store-name { display: block; font-weight: 600; font-size: 14px; }
.ss-store-sub {
  display: block;
  font-size: 11.5px;
  color: var(--faint);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ss-store.blocked { box-shadow: inset 3px 0 0 var(--danger); }

.ss-count { font-size: 12px; font-weight: 600; color: var(--accent-deep); white-space: nowrap; }
.ss-count.ok { color: var(--ok); display: inline-flex; }
.ss-count.ok :deep(svg) { width: 15px; height: 15px; }
</style>
