<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Validation status: pre-validation → sandbox → production. Conformance
// records are pinned to a spec version. Triggering a pre-validation run is
// the only action available to the merchant; sandbox/production are gated
// by Google.
//
// Endpoints:
//   GET  /validation/status
//   GET  /validation/runs
//   POST /validation/runs   (trigger a pre-validation)

import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useValidationStore } from '@/stores/validation'
import Chip, { type ChipKind } from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const validation = useValidationStore()
const { t } = useI18n()

onMounted(() => validation.load())

const showSkel = computed(() => !validation.loaded && validation.loading)

const PRE_KIND: Record<string, ChipKind> = { not_run: 'quiet', passing: 'ok', failing: 'danger' }
const SANDBOX_KIND: Record<string, ChipKind> = { not_submitted: 'quiet', pending: 'warn', passed: 'ok', failed: 'danger' }
const PROD_KIND: Record<string, ChipKind> = { not_submitted: 'quiet', pending: 'warn', live: 'ok' }

const status = computed(() => validation.status)

async function onTrigger() {
  await validation.triggerRun()
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="head-row">
        <div>
          <div class="eyebrow">{{ t('validation.eyebrow') }}</div>
          <h1 class="page-title display">{{ t('validation.title') }}</h1>
          <p class="page-sub">
            {{ t('validation.sub') }}
          </p>
        </div>
        <button
          class="btn btn-primary"
          type="button"
          :disabled="validation.triggering"
          @click="onTrigger"
        >
          <Icon name="zap" />
          {{ validation.triggering ? t('validation.running') : t('validation.runButton') }}
        </button>
      </div>
    </div>

    <ErrorBanner v-if="validation.error" :message="validation.error" @retry="validation.load" />

    <template v-if="showSkel">
      <Skeleton height="120px" radius="11px" />
      <Skeleton height="180px" radius="11px" />
    </template>

    <template v-else-if="status">
      <div class="stages">
        <div class="stage">
          <div class="stage-label">{{ t('validation.stages.preValidation') }}</div>
          <Chip :kind="PRE_KIND[status.pre_validation ?? 'not_run']">{{ status.pre_validation }}</Chip>
        </div>
        <div class="stage-arrow"><Icon name="arrowR" /></div>
        <div class="stage">
          <div class="stage-label">{{ t('validation.stages.googleSandbox') }}</div>
          <Chip :kind="SANDBOX_KIND[status.sandbox ?? 'not_submitted']">{{ status.sandbox }}</Chip>
        </div>
        <div class="stage-arrow"><Icon name="arrowR" /></div>
        <div class="stage">
          <div class="stage-label">{{ t('validation.stages.production') }}</div>
          <Chip :kind="PROD_KIND[status.production ?? 'not_submitted']">{{ status.production }}</Chip>
        </div>
      </div>

      <div class="meta">
        <template v-if="status.last_run_at">
          {{ t('validation.lastRun') }} <span class="mono">{{ status.last_run_at }}</span>
        </template>
        <details class="tech">
          <summary>{{ t('validation.targetSpec') }}</summary>
          <span>{{ t('validation.checkedAgainst') }} <span class="mono">{{ status.target_spec_version }}</span></span>
        </details>
      </div>

      <section v-if="(status.open_failures?.length ?? 0) > 0" class="section">
        <div class="section-label">{{ t('validation.openFailures') }}<span class="ln" /></div>
        <div class="card list">
          <div
            v-for="(f, i) in status.open_failures"
            :key="i"
            class="row"
            :class="{ first: i === 0 }"
          >
            <span class="dot dot-warn" />
            <div class="row-main">
              <div class="row-title">{{ f.summary || t('validation.genericFailureTitle') }}</div>
              <details class="tech row-sub">
                <summary>{{ t('common.technicalDetails') }}</summary>
                <span class="mono">{{ f.requirement }}</span>
                <span v-if="f.spec_section" class="sec">{{ f.spec_section }}</span>
              </details>
            </div>
            <Chip kind="danger">{{ t('validation.failure') }}</Chip>
          </div>
        </div>
      </section>

      <section class="section">
        <div class="section-label">{{ t('validation.runHistory') }}<span class="ln" /></div>
        <div v-if="validation.runs.length === 0" class="empty">{{ t('validation.noRuns') }}</div>
        <div v-else class="card list">
          <div
            v-for="(r, i) in validation.runs"
            :key="r.run_id"
            class="row"
            :class="{ first: i === 0 }"
          >
            <div class="row-main">
              <div class="row-title">{{ t('validation.runOn', { when: r.ran_at }) }}</div>
              <div class="row-sub">
                <span class="mono ok">{{ r.passed }} {{ t('validation.pass') }}</span>
                · <span class="mono warn">{{ r.failed }} {{ t('validation.fail') }}</span>
                · <span class="mono muted">{{ r.warnings }} {{ t('validation.warn') }}</span>
              </div>
              <details class="tech row-sub">
                <summary>{{ t('common.technicalDetails') }}</summary>
                <span class="mono">{{ r.run_id }}</span>
                <span class="sec">{{ r.spec_version }}</span>
              </details>
            </div>
            <Chip :kind="r.result === 'pass' ? 'ok' : 'danger'">{{ r.result }}</Chip>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 24px; }
.head-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 20px;
  flex-wrap: wrap;
}
.page-title { font-size: 30px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: var(--radius-sm);
  padding: 10px 16px;
  font-weight: 600;
  font-size: 13px;
  border: 0;
  cursor: pointer;
  white-space: nowrap;
}
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-deep); }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn :deep(svg) { width: 14px; height: 14px; }

.stages {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  padding: 18px 22px;
  box-shadow: var(--shadow);
  flex-wrap: wrap;
}
.stage { display: flex; flex-direction: column; gap: 6px; min-width: 140px; }
.stage-label { font-size: 11.5px; color: var(--faint); letter-spacing: 0.06em; text-transform: uppercase; }
.stage-arrow { color: var(--faint); }
.stage-arrow :deep(svg) { width: 16px; height: 16px; }

.meta {
  color: var(--muted);
  font-size: 13px;
  margin-top: 10px;
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.mono { font-family: var(--font-mono); }

.tech { margin-top: 4px; }
.tech summary {
  cursor: pointer;
  color: var(--text-faint);
  font-size: 12px;
  user-select: none;
}
.tech summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.tech[open] summary { margin-bottom: 6px; }
.tech span + span { margin-left: 8px; }

.section { margin-top: 28px; }
.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
  margin-bottom: 10px;
}
.section-label .ln { flex: 1; height: 1px; background: var(--line); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.list { overflow: hidden; }
.empty { padding: 20px; color: var(--muted); text-align: center; }

.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-warn { background: var(--warn); }

.row-main { flex: 1; min-width: 0; }
.row-title { font-weight: 600; display: inline-flex; align-items: center; gap: 10px; }
.row-title .sec { color: var(--faint); font-weight: 400; font-size: 12.5px; }
.row-sub { color: var(--muted); font-size: 12.5px; margin-top: 4px; display: flex; gap: 4px; flex-wrap: wrap; }
.row-sub .ok { color: var(--ok); }
.row-sub .warn { color: var(--danger); }
.row-sub .muted { color: var(--muted); }
</style>
