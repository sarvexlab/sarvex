<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Connect wizard — final step: pairing succeeded, hand off to the Path.

import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import LoadingButton from '@/components/ui/LoadingButton.vue'

defineProps<{ storeName: string }>()

const emit = defineEmits<{ done: [] }>()

const { t } = useI18n()
</script>

<template>
  <div class="paired">
    <div class="paired-icon"><Icon name="check" :size="24" /></div>
    <h1 class="display title">{{ t('connect.paired.title', { storeName }) }}</h1>
    <p class="lede center">
      {{ t('connect.paired.lede', { storeName }) }}
    </p>
    <LoadingButton class="done-btn" @click="emit('done')">
      {{ t('connect.paired.openPath', { storeName }) }}
      <template #after><Icon name="arrowR" /></template>
    </LoadingButton>
  </div>
</template>

<style scoped>
.title { font-size: 28px; margin-top: 6px; }
.lede {
  color: var(--muted);
  margin-top: 10px;
  max-width: 60ch;
}
.lede.center { margin: 10px auto 0; max-width: 38ch; text-align: center; }

.paired { text-align: center; padding: 14px 4px 6px; }
.paired-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  margin: 0 auto 18px;
  background: color-mix(in oklab, var(--ok) 14%, var(--surface));
  color: var(--ok);
}
.done-btn { margin-top: 24px; padding: 12px 18px; }
</style>
