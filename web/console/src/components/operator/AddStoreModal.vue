<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Operator: add a store to a merchant. Server creates the store and merchant
// can then pair it via the ConnectSurface wizard.

import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'
import { DEFAULT_STORE_PLATFORM, STORE_PLATFORMS, type StorePlatform } from '@/platforms'

const props = defineProps<{ merchantId: string; merchantName: string }>()
const emit = defineEmits<{ close: []; created: [storeId: string] }>()

const operator = useOperatorStore()
const { t } = useI18n()

const name = ref('')
const platform = ref<StorePlatform>(DEFAULT_STORE_PLATFORM)
const submitting = ref(false)

const valid = computed(() => name.value.trim().length > 1)

async function onCreate() {
  if (!valid.value || submitting.value) return
  submitting.value = true
  try {
    const store = await operator.createStore(props.merchantId, name.value.trim(), platform.value)
    if (store.store_id) emit('created', store.store_id)
  } finally {
    submitting.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="scrim" @click="emit('close')" />
  <div class="modal" role="dialog" aria-modal="true">
    <div class="head">
      <div>
        <div class="eyebrow">{{ t('operator.addStoreModal.eyebrow') }}</div>
        <h2 class="display title">{{ t('operator.addStoreModal.title') }}</h2>
        <p class="head-sub">
          {{ t('operator.addStoreModal.toMerchant') }} <strong>{{ merchantName }}</strong>
        </p>
      </div>
      <button type="button" class="icon-btn" :aria-label="t('common.close')" @click="emit('close')">
        <Icon name="x" />
      </button>
    </div>
    <div class="body">
      <Field :label="t('operator.addStoreModal.storeNameLabel')">
        <input v-model="name" type="text" :placeholder="t('operator.addStoreModal.storeNamePlaceholder')" />
      </Field>
      <Field :label="t('operator.addStoreModal.platformLabel')">
        <select v-model="platform">
          <option v-for="(label, p) in STORE_PLATFORMS" :key="p" :value="p">{{ t(label) }}</option>
        </select>
      </Field>
      <p class="info-text">
        {{ t('operator.addStoreModal.infoTextPrefix') }} {{ t('operator.addStoreModal.infoText', { merchantName }) }}
      </p>
    </div>
    <div class="foot">
      <button type="button" class="btn btn-ghost" @click="emit('close')">{{ t('operator.addStoreModal.cancel') }}</button>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="!valid || submitting"
        @click="onCreate"
      >
        <Icon name="plus" /> {{ submitting ? t('operator.addStoreModal.adding') : t('operator.addStoreModal.addToBoard') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  background: color-mix(in oklab, var(--ink) 32%, transparent);
  backdrop-filter: blur(2px);
  z-index: 60;
}
.modal {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 61;
  width: min(460px, 94vw);
  max-height: 90vh;
  overflow-y: auto;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow-lg);
}
.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 22px 24px 14px;
}
.eyebrow { margin-bottom: 6px; }
.title { font-size: 22px; }
.head-sub { color: var(--muted); font-size: 13px; margin-top: 5px; }
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
.body { padding: 4px 24px 8px; }
.info-text {
  margin-top: 14px;
  padding: 0;
  color: var(--muted);
  font-size: 13.5px;
  line-height: 1.5;
}

.foot {
  display: flex;
  gap: 10px;
  padding: 16px 24px 20px;
}
.btn {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: var(--radius-sm);
  padding: 11px 16px;
  font-weight: 600;
  font-size: var(--fs-small);
  border: 0;
  cursor: pointer;
}
.btn-sm { flex: 0; padding: 6px 12px; font-size: 12.5px; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn-ghost { background: var(--surface); box-shadow: inset 0 0 0 1px var(--line2); color: var(--ink); }
</style>
