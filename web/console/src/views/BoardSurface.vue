<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The operator kanban — the only operator-exclusive screen. Columns are
// the journey states; cards are stores. Click → enter-as-merchant.
//
// Endpoints:
//   GET /operator/board       → useBoardStore
//   GET /operator/merchants   → useOperatorStore (for the roster strip)
//   POST .../merchants        → NewMerchantModal
//   POST .../{id}/stores      → AddStoreModal

import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useBoardStore } from '@/stores/board'
import { useOperatorStore } from '@/stores/operator'
import MerchantCard from '@/components/operator/MerchantCard.vue'
import NewMerchantModal from '@/components/operator/NewMerchantModal.vue'
import AddStoreModal from '@/components/operator/AddStoreModal.vue'
import ManualStepModal from '@/components/operator/ManualStepModal.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'
import Icon from '@/components/ui/Icon.vue'
import type { components } from '@/api/schema'
import { platformLabelKey } from '@/platforms'

type BoardCard = components['schemas']['board_card']

const board = useBoardStore()
const operator = useOperatorStore()
const router = useRouter()
const { t } = useI18n()

const showNewMerchant = ref(false)
const addStoreFor = ref<{ id: string; name: string } | null>(null)
const showManualStep = ref(false)
const toast = ref<string | null>(null)
let toastTimer: number | null = null

function flashToast(msg: string) {
  toast.value = msg
  if (toastTimer !== null) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 2600)
}

async function refresh() {
  await Promise.all([board.load(), operator.loadMerchants()])
}
onMounted(refresh)

const stages = computed(() => board.stages)
const byStage = computed(() => board.byStage)
const total = computed(() => board.cards.length)
const open = computed(() => board.totalOpen)
const blocked = computed(() => board.blockedCount)
const showSkeleton = computed(() => !board.loaded && board.loading)

function enterCard(card: BoardCard) {
  if (!card.merchant_id || !card.store_id) return
  operator.enterMerchant(
    card.merchant_id,
    card.store_id,
    card.name,
    card.name,
  )
  router.push(`/board/${card.merchant_id}/${card.store_id}`)
}

async function onMerchantCreated() {
  showNewMerchant.value = false
  await refresh()
}

async function onStoreCreated() {
  addStoreFor.value = null
  await refresh()
}

function onStepCaptured() {
  showManualStep.value = false
  flashToast(t('board.manualStepLogged'))
}
</script>

<template>
  <div class="page page-wide">
    <div class="page-head">
      <div>
        <div class="eyebrow">{{ t('board.eyebrow') }}</div>
        <h1 class="page-title display">{{ t('board.title') }}</h1>
        <p class="page-sub">
          {{ t('board.subPrefix') }}
          <strong>{{ total }}</strong> {{ t('board.storesWord') }} ·
          <strong>{{ open }}</strong> {{ t('board.openTasksWord') }}<template v-if="blocked > 0">
            · <strong>{{ blocked }}</strong> {{ t('board.blockedWord') }}</template>.
        </p>
      </div>
      <div class="head-cards">
        <div class="card stat">
          <div class="display big">{{ total }}</div>
          <div class="stat-label">{{ t('board.active') }}</div>
        </div>
        <div :class="['card', 'stat', { warn: blocked > 0 }]">
          <div :class="['display', 'big', { warn: blocked > 0 }]">{{ blocked }}</div>
          <div class="stat-label">{{ t('board.blocked') }}</div>
        </div>
      </div>
    </div>

    <ErrorBanner v-if="board.error" :message="board.error" @retry="refresh" />

    <div v-if="showSkeleton" class="board">
      <div v-for="s in stages" :key="s" class="bcol-wrap">
        <div class="bcol-head">
          <span class="bname"><Skeleton width="80px" height="12px" /></span>
        </div>
        <div class="bcol">
          <Skeleton v-for="i in 2" :key="i" height="86px" radius="11px" />
        </div>
      </div>
    </div>
    <div v-else class="board">
      <div v-for="s in stages" :key="s" class="bcol-wrap">
        <div class="bcol-head">
          <span class="bname">{{ t(`board.stage.${s}`) }}</span>
          <span class="bcount">{{ byStage[s].length }}</span>
        </div>
        <div class="bcol">
          <MerchantCard
            v-for="c in byStage[s]"
            :key="c.merchant_id"
            :card="c"
            @open="enterCard"
          />
        </div>
      </div>
    </div>

    <div class="section-label">
      <Icon name="user" /> {{ t('board.merchants') }}
      <span class="count">· {{ operator.merchants.length }}</span>
      <span class="ln" />
      <button type="button" class="btn-ghost" @click="showNewMerchant = true">
        <Icon name="plus" /> {{ t('board.newMerchant') }}
      </button>
    </div>
    <div class="card list">
      <div
        v-for="(m, i) in operator.merchants"
        :key="m.merchant_id"
        class="mrow"
        :class="{ first: i === 0 }"
      >
        <span class="avatar">{{ (m.name ?? '?').slice(0, 1).toUpperCase() }}</span>
        <div class="mr-main">
          <div class="mr-name">{{ m.name }}</div>
          <div class="mr-sub mono">{{ m.merchant_id }}</div>
          <div v-if="operator.storesByMerchant[m.merchant_id ?? '']?.length" class="store-list">
            <span
              v-for="s in operator.storesByMerchant[m.merchant_id ?? '']"
              :key="s.store_id"
              class="store-chip"
            >
              {{ s.name }} <span>· {{ t(platformLabelKey(s.platform)) }}</span>
            </span>
          </div>
        </div>
        <button
          type="button"
          class="btn-ghost"
          @click="addStoreFor = { id: m.merchant_id ?? '', name: m.name ?? '' }"
        >
          <Icon name="plus" /> {{ t('board.addStore') }}
        </button>
      </div>
    </div>

    <div class="section-label">
      <Icon name="flag" /> {{ t('board.capturedManualSteps') }}
      <span class="count">· {{ t('board.thisSession') }} · {{ operator.capturedSteps.length }}</span>
      <span class="ln" />
      <button type="button" class="btn-ghost" @click="showManualStep = true">
        <Icon name="plus" /> {{ t('board.captureStep') }}
      </button>
    </div>
    <div v-if="operator.capturedSteps.length === 0" class="card empty-steps">
      {{ t('board.noStepsCapturedPrefix') }} <strong>{{ t('board.captureStep') }}</strong> {{ t('board.noStepsCapturedSuffix') }}
    </div>
    <div v-else class="card list">
      <div
        v-for="(s, i) in operator.capturedSteps"
        :key="s.manual_step_id"
        class="mrow"
        :class="{ first: i === 0 }"
      >
        <span class="step-dot"><Icon name="note" /></span>
        <div class="mr-main">
          <div class="mr-name">{{ s.description }}</div>
          <div class="mr-sub">
            <span class="mono">{{ s.disposition }}</span>
            <template v-if="s.target_merchant_id">
              · <span class="mono">{{ s.target_merchant_id }}</span>
            </template>
            <template v-if="s.created_at"> · {{ s.created_at }}</template>
          </div>
        </div>
      </div>
    </div>

    <NewMerchantModal
      v-if="showNewMerchant"
      @close="showNewMerchant = false"
      @created="onMerchantCreated"
    />
    <AddStoreModal
      v-if="addStoreFor"
      :merchant-id="addStoreFor.id"
      :merchant-name="addStoreFor.name"
      @close="addStoreFor = null"
      @created="onStoreCreated"
    />
    <ManualStepModal
      v-if="showManualStep"
      @close="showManualStep = false"
      @captured="onStepCaptured"
    />

    <div v-if="toast" class="toast">
      <Icon name="check" />
      <span>{{ toast }}</span>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1280px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
  margin-bottom: 30px;
}
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.head-cards { display: flex; gap: 10px; }
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.stat { padding: 12px 18px; text-align: center; }
.stat.warn { border-color: color-mix(in oklab, var(--danger) 40%, var(--line)); }
.stat .big { font-size: 24px; }
.stat .big.warn { color: var(--danger); }
.stat-label {
  font-size: 11.5px;
  color: var(--faint);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.board {
  display: grid;
  grid-template-columns: repeat(6, minmax(180px, 1fr));
  gap: 14px;
  align-items: start;
  overflow-x: auto;
  padding-bottom: 10px;
}
.bcol-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px 12px;
}
.bname {
  font-size: 12.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}
.bcount {
  font-size: 12px;
  color: var(--faint);
  background: var(--surface2);
  border-radius: 99px;
  padding: 1px 8px;
}
.bcol {
  display: flex;
  flex-direction: column;
  gap: 11px;
  min-height: 60px;
}

.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 40px 4px 13px;
}
.section-label :deep(svg) { width: 14px; height: 14px; }
.section-label .ln { flex: 1; height: 1px; background: var(--line); }
.section-label .count { color: var(--accent-deep); }

.list { overflow: hidden; max-width: 640px; }
.mrow {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 20px;
  border-top: 1px solid var(--line);
}
.mrow.first { border-top: 0; }
.avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--surface2);
  color: var(--muted);
  font: 600 14px/1 var(--font-ui);
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.mr-main { flex: 1; min-width: 0; }
.mr-name { font-weight: 600; }
.mr-sub { font-size: 12.5px; color: var(--faint); margin-top: 2px; }
.store-list { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
.store-chip {
  font-size: 12px;
  color: var(--muted);
  background: var(--surface2);
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 2px 8px;
}

.btn-ghost {
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
  border: 0;
  border-radius: var(--radius-sm);
  padding: 6px 12px;
  font-weight: 600;
  font-size: 12.5px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.btn-ghost:hover { box-shadow: inset 0 0 0 1px var(--faint); }
.btn-ghost :deep(svg) { width: 13px; height: 13px; }

.empty-steps {
  padding: 24px;
  color: var(--muted);
  font-size: 13.5px;
  max-width: 640px;
  text-align: center;
  line-height: 1.6;
}
.empty-steps strong { color: var(--ink); }

.step-dot {
  width: 30px;
  height: 30px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: var(--surface2);
  color: var(--muted);
  flex-shrink: 0;
}
.step-dot :deep(svg) { width: 14px; height: 14px; }

.toast {
  position: fixed;
  bottom: 84px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 80;
  background: color-mix(in srgb, var(--ink) 93%, transparent);
  color: #fff;
  font-size: 13.5px;
  font-weight: 500;
  padding: 11px 20px;
  border-radius: 99px;
  box-shadow: 0 14px 44px -12px rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  gap: 9px;
}
.toast :deep(svg) { width: 15px; height: 15px; color: var(--ok); }
</style>
