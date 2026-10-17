<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The brief asks for a merchant-initiated "Go live" confirm-and-celebrate
// screen. There is no such action in this product: `verified → active` is
// driven automatically by endpointhealth's probes (sarvex-v1-architecture.md
// Part A, `activation` row) — there is nothing for a merchant to click, and
// no backend mutation to call. What's real, and what merchants actually
// want to see, is the moment it happens: this watches the activation state
// this shell already loads (kept live by the journey stream's
// onActivationUpdate) and shows a one-time, restrained success state the
// instant the store crosses into `active`. Mounted once by AppShell.
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useActivationStore } from '@/stores/activation'
import { useStoresStore } from '@/stores/stores'
import Icon from '@/components/ui/Icon.vue'

const activation = useActivationStore()
const stores = useStoresStore()
const { t } = useI18n()

const visible = ref(false)
let prev: string | null = null

watch(
  () => activation.activation?.ucp_state,
  (next) => {
    if (prev && prev !== 'active' && next === 'active') {
      visible.value = true
    }
    prev = next ?? null
  },
)

// A store switch shouldn't carry a stale "previous state" across into the
// next store's activation record.
watch(
  () => stores.currentStoreId,
  () => { prev = null },
)

let hideTimer: number | null = null
watch(visible, (v) => {
  if (hideTimer !== null) window.clearTimeout(hideTimer)
  if (v) hideTimer = window.setTimeout(() => { visible.value = false }, 6000)
})
onUnmounted(() => { if (hideTimer !== null) window.clearTimeout(hideTimer) })
</script>

<template>
  <Teleport to="body">
    <div v-if="visible" class="golive-overlay" role="status" @click="visible = false">
      <div class="golive-card">
        <span class="glow" aria-hidden="true" />
        <div class="icon"><Icon name="zap" :size="22" /></div>
        <h2 class="display title">{{ t('connect.golive.title', { storeName: stores.currentStore?.name ?? '' }) }}</h2>
        <p class="sub">{{ t('connect.golive.sub') }}</p>
        <button type="button" class="btn btn-secondary" @click="visible = false">{{ t('common.close') }}</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.golive-overlay {
  position: fixed;
  inset: 0;
  z-index: 300;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
  padding: 16px;
}
.golive-card {
  position: relative;
  width: 100%;
  max-width: 400px;
  text-align: center;
  padding: 36px 28px 28px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.glow {
  position: absolute;
  inset: -40% -40% auto -40%;
  height: 220px;
  background: radial-gradient(closest-side, var(--accent-glow, var(--accent-soft)), transparent);
  pointer-events: none;
}
.icon {
  position: relative;
  width: 48px;
  height: 48px;
  margin: 0 auto 16px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
}
.title { position: relative; font-size: 22px; }
.sub { position: relative; color: var(--text-muted); font-size: 14px; margin-top: 8px; }
.btn { margin-top: 22px; }

@media (prefers-reduced-motion: no-preference) {
  .golive-card { animation: golive-in 0.3s var(--ease-out, ease-out); }
}
@keyframes golive-in {
  from { opacity: 0; transform: translateY(8px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}
</style>
