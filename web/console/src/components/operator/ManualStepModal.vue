<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Capture an action the product couldn't do yet. Operator-only. The
// disposition lets us tag it for the Phase-B automation backlog (automate
// vs templatise vs accept-manual).

import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import type { components } from '@/api/schema'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'

type Disposition = NonNullable<components['schemas']['manual_step']['disposition']>

const props = defineProps<{ targetMerchantId?: string }>()
const emit = defineEmits<{ close: []; captured: [] }>()

const operator = useOperatorStore()
const { t } = useI18n()

const description = ref('')
const disposition = ref<Disposition>('unclassified')
const submitting = ref(false)

const valid = computed(() => description.value.trim().length > 3)

const DISPOSITIONS = computed<Array<{ value: Disposition; label: string; sub: string }>>(() => [
  { value: 'unclassified', label: t('operator.manualStepModal.dispositions.unclassified.label'), sub: t('operator.manualStepModal.dispositions.unclassified.sub') },
  { value: 'automate',     label: t('operator.manualStepModal.dispositions.automate.label'),     sub: t('operator.manualStepModal.dispositions.automate.sub') },
  { value: 'templatise',   label: t('operator.manualStepModal.dispositions.templatise.label'),   sub: t('operator.manualStepModal.dispositions.templatise.sub') },
  { value: 'accept_manual',label: t('operator.manualStepModal.dispositions.accept_manual.label'),sub: t('operator.manualStepModal.dispositions.accept_manual.sub') },
])

async function onSubmit() {
  if (!valid.value || submitting.value) return
  submitting.value = true
  try {
    await operator.captureManualStep({
      description: description.value.trim(),
      disposition: disposition.value,
      ...(props.targetMerchantId ? { target_merchant_id: props.targetMerchantId } : {}),
    })
    emit('captured')
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
        <div class="eyebrow">{{ t('operator.manualStepModal.eyebrow') }}</div>
        <h2 class="display title">{{ t('operator.manualStepModal.title') }}</h2>
      </div>
      <button class="icon-btn" type="button" :aria-label="t('common.close')" @click="emit('close')">
        <Icon name="x" />
      </button>
    </div>
    <div class="body">
      <p class="lede">
        {{ t('operator.manualStepModal.lede') }}
      </p>
      <Field :label="t('operator.manualStepModal.whatDidYouDoLabel')">
        <textarea
          v-model="description"
          rows="3"
          :placeholder="t('operator.manualStepModal.descriptionPlaceholder')"
          @keydown.enter.meta="onSubmit"
        />
      </Field>
      <Field :label="t('operator.manualStepModal.dispositionLabel')">
        <div class="grid">
          <button
            v-for="d in DISPOSITIONS"
            :key="d.value"
            type="button"
            :class="['opt', { sel: disposition === d.value }]"
            @click="disposition = d.value"
          >
            <span class="opt-label">{{ d.label }}</span>
            <span class="opt-sub">{{ d.sub }}</span>
          </button>
        </div>
      </Field>
    </div>
    <div class="foot">
      <button type="button" class="btn btn-ghost" @click="emit('close')">{{ t('operator.addStoreModal.cancel') }}</button>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="!valid || submitting"
        @click="onSubmit"
      >
        <Icon name="check" /> {{ submitting ? t('operator.manualStepModal.logging') : t('operator.manualStepModal.logStep') }}
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
  width: min(520px, 94vw);
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
.body { padding: 4px 24px 8px; }
.lede { color: var(--muted); font-size: 13.5px; line-height: 1.55; margin-bottom: 18px; }

.grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}
.opt {
  background: var(--surface);
  border: 1px solid var(--line2);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  text-align: left;
  cursor: pointer;
  color: inherit;
  font: inherit;
}
.opt:hover { border-color: var(--faint); }
.opt.sel { border-color: var(--accent); background: var(--accent-softer); }
.opt-label { display: block; font-weight: 600; font-size: 13px; }
.opt-sub { display: block; color: var(--muted); font-size: 12px; margin-top: 2px; }

.foot { display: flex; gap: 10px; padding: 16px 24px 20px; }
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
