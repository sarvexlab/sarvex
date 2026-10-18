<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Connect wizard — step 1: install the plugin, then generate a pairing code.

import { useI18n } from 'vue-i18n'
import Mark from '@/components/shell/Mark.vue'
import LoadingButton from '@/components/ui/LoadingButton.vue'

defineProps<{
  storeName: string
  domain: string
  platformLabel: string
  generating: boolean
}>()

const emit = defineEmits<{ generate: [] }>()

const { t } = useI18n()
</script>

<template>
  <div class="head">
    <div class="brandline"><Mark /> {{ t('connect.brandline') }}</div>
    <h1 class="display title">{{ t('connect.idle.title', { storeName }) }}</h1>
    <p class="lede">
      {{ t('connect.idle.lede', { storeName }) }}
    </p>
  </div>

  <ol class="step-list">
    <li>
      <div>
        <strong>{{ t('connect.idle.step1.titleStrong') }}</strong>
        {{ t('connect.idle.step1.inAdmin', { storeName, platformLabel }) }}
        <p class="step-sub">
          {{ t('connect.idle.step1.subPrefix') }}
          <a href="#" class="inline-link">{{ t('connect.idle.step1.downloadZip') }}</a>{{ t('connect.idle.step1.subSuffix', { domain }) }}
        </p>
      </div>
    </li>
    <li>
      <div>
        <strong>{{ t('connect.idle.step2.titleStrong') }}</strong>
        <p class="step-sub">
          {{ t('connect.idle.step2.sub') }}
        </p>
        <LoadingButton icon="zap" :loading="generating" @click="emit('generate')">
          {{ generating ? t('connect.idle.step2.generating') : t('connect.idle.step2.generate') }}
        </LoadingButton>
      </div>
    </li>
  </ol>
</template>

<style scoped>
.head { margin-bottom: 22px; }
.brandline {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 13px;
  letter-spacing: -0.01em;
  color: var(--muted);
}
.title { font-size: 28px; margin-top: 6px; }
.lede {
  color: var(--muted);
  margin-top: 10px;
  max-width: 60ch;
}

.step-list {
  list-style: none;
  padding: 0;
  margin: 16px 0 0;
  display: flex;
  flex-direction: column;
  counter-reset: s;
}
.step-list li {
  display: flex;
  gap: 14px;
  padding: 13px 0;
  align-items: flex-start;
}
.step-list li::before {
  counter-increment: s;
  content: counter(s);
  flex-shrink: 0;
  width: 25px;
  height: 25px;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent-deep);
  font-size: 12.5px;
  font-weight: 600;
  display: grid;
  place-items: center;
}
.step-list li + li { border-top: 1px solid var(--line); }
.step-sub {
  color: var(--muted);
  font-size: 13.5px;
  margin: 4px 0 12px;
  line-height: 1.5;
}
.inline-link { color: var(--accent-deep); font-weight: 600; }
.inline-link:hover { text-decoration: underline; }
</style>
