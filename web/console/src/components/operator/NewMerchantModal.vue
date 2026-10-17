<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Operator: create a merchant + invite the owner. Provisioning is per
// merchant, not per store. The store gets added afterward from the new
// card via AddStoreModal.

import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'

const operator = useOperatorStore()
const emit = defineEmits<{ close: []; created: [merchantId: string] }>()

const { t } = useI18n()

const name = ref('')
const email = ref('')
const submitting = ref(false)

const valid = computed(
  () => name.value.trim().length > 1 && /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.value.trim()),
)

async function onCreate() {
  if (!valid.value || submitting.value) return
  submitting.value = true
  try {
    const acct = await operator.createMerchant(name.value.trim(), email.value.trim())
    if (acct.merchant_id) {
      emit('created', acct.merchant_id)
    }
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
        <div class="eyebrow">{{ t('operator.newMerchantModal.eyebrow') }}</div>
        <h2 class="display title">{{ t('operator.newMerchantModal.title') }}</h2>
      </div>
      <button type="button" class="icon-btn" :aria-label="t('common.close')" @click="emit('close')">
        <Icon name="x" />
      </button>
    </div>
    <div class="body">
      <p class="lede">
        {{ t('operator.newMerchantModal.lede') }}
      </p>
      <Field :label="t('operator.newMerchantModal.merchantNameLabel')">
        <input v-model="name" type="text" :placeholder="t('operator.newMerchantModal.merchantNamePlaceholder')" />
      </Field>
      <Field :label="t('operator.newMerchantModal.adminEmailLabel')" :hint="t('operator.newMerchantModal.adminEmailHint')">
        <input
          v-model="email"
          type="email"
          :placeholder="t('operator.newMerchantModal.adminEmailPlaceholder')"
          @keydown.enter="onCreate"
        />
      </Field>
    </div>
    <div class="foot">
      <button type="button" class="btn btn-ghost" @click="emit('close')">{{ t('operator.newMerchantModal.cancel') }}</button>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="!valid || submitting"
        @click="onCreate"
      >
        <Icon name="mail" /> {{ submitting ? t('operator.newMerchantModal.sending') : t('operator.newMerchantModal.sendInvite') }}
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
  width: min(440px, 94vw);
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
.body { padding: 4px 24px 8px; }
.lede {
  color: var(--muted);
  font-size: 13.5px;
  line-height: 1.55;
  margin-bottom: 18px;
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
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn-ghost { background: var(--surface); box-shadow: inset 0 0 0 1px var(--line2); color: var(--ink); }
</style>
