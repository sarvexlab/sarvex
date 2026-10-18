<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Drawer content for the pre-validation milestone/failure tasks (see
// registry.ts's pre_validation entry) — the actual "action" TaskDrawer.vue
// was missing for these: status, open failures, run history, and the
// trigger button itself, ported from ValidationView.vue/stores/validation.ts
// (unchanged, still reachable directly at /validation).
//
// Triggering a run isn't "resolving" this task the way TaskDrawer's shared
// footer button resolves a merchant-owned task — this task is owner=sarvex
// and retires itself server-side the moment any run completes, pass or
// fail (validation.Orchestrator.RunValidation). So the button lives here,
// not in the shared footer, and — same as the drawer's existing remediation
// apply flow — doesn't force a task-list refresh after acting; the panel's
// own status/history update from the same store call.
//
// One run covers both halves of "is this store ready": the suite against the
// catalog and profile, and the verification of the store's own UCP endpoints
// that advances activation to verified. The endpoint checks are read from the
// activation record (last_verification) rather than from a verify call of our
// own, since the server runs them as part of the pre-validation run.

import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useValidationStore } from '@/stores/validation'
import { useActivationStore } from '@/stores/activation'
import Chip, { type ChipKind } from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'

const validation = useValidationStore()
const activation = useActivationStore()
const { t } = useI18n()

onMounted(() => {
  if (!validation.loaded && !validation.loading) validation.load()
  if (!activation.loaded && !activation.loading) activation.load()
})

const PRE_KIND: Record<string, ChipKind> = { not_run: 'quiet', passing: 'ok', failing: 'danger' }

const verification = computed(() => activation.activation?.last_verification ?? null)
</script>

<template>
  <div class="section">
    <button
      class="btn btn-primary block"
      type="button"
      :disabled="validation.triggering"
      @click="validation.triggerRun()"
    >
      <Icon name="zap" />
      {{ validation.triggering ? t('task.widgets.preValidationPanel.running') : t('task.widgets.preValidationPanel.runButton') }}
    </button>
    <p v-if="validation.error" class="error-text">{{ validation.error }}</p>
  </div>

  <template v-if="validation.status">
    <div class="section">
      <div class="status-row">
        <span class="eyebrow">{{ t('task.widgets.preValidationPanel.preValidationLabel') }}</span>
        <Chip :kind="PRE_KIND[validation.status.pre_validation ?? 'not_run']">
          {{ validation.status.pre_validation }}
        </Chip>
      </div>
      <p v-if="validation.status.last_run_at" class="muted">
        {{ t('task.widgets.preValidationPanel.lastRunLabel') }} <span class="mono">{{ validation.status.last_run_at }}</span>
      </p>
    </div>

    <div v-if="(validation.status.open_failures?.length ?? 0) > 0" class="section">
      <div class="eyebrow">{{ t('task.widgets.preValidationPanel.openFailures') }}</div>
      <div class="card list">
        <div
          v-for="(f, i) in validation.status.open_failures"
          :key="i"
          class="row"
          :class="{ first: i === 0 }"
        >
          <div class="row-main">
            <div class="row-title">{{ f.summary || t('validation.genericFailureTitle') }}</div>
            <details class="tech">
              <summary>{{ t('common.technicalDetails') }}</summary>
              <span class="mono">{{ f.requirement }}</span>
              <span v-if="f.spec_section" class="row-sep">{{ f.spec_section }}</span>
            </details>
          </div>
          <Chip kind="danger">{{ t('task.widgets.preValidationPanel.failure') }}</Chip>
        </div>
      </div>
    </div>

    <div class="section">
      <div class="status-row">
        <span class="eyebrow">{{ t('task.widgets.preValidationPanel.endpointChecks') }}</span>
        <Chip v-if="verification" :kind="verification.passed ? 'ok' : 'danger'">
          {{ verification.passed ? t('task.widgets.preValidationPanel.pass') : t('task.widgets.preValidationPanel.fail') }}
        </Chip>
      </div>
      <p v-if="!verification" class="muted">
        {{ t('task.widgets.preValidationPanel.endpointChecksEmpty') }}
      </p>
      <template v-else>
        <div v-if="verification.checks.length > 0" class="card list">
          <div
            v-for="(c, i) in verification.checks"
            :key="c.check_id"
            class="row"
            :class="{ first: i === 0 }"
          >
            <div class="row-main">
              <div class="row-title">{{ c.detail || t('task.widgets.preValidationPanel.genericCheckTitle') }}</div>
              <details class="tech">
                <summary>{{ t('common.technicalDetails') }}</summary>
                <span class="mono">{{ c.check_id }}</span>
              </details>
            </div>
            <Chip :kind="c.passed ? 'ok' : 'danger'">
              {{ c.passed ? t('task.widgets.preValidationPanel.checkPass') : t('task.widgets.preValidationPanel.checkFail') }}
            </Chip>
          </div>
        </div>
        <p class="muted">
          {{ t('task.widgets.preValidationPanel.endpointChecksRanAt') }}
          <span class="mono">{{ verification.ran_at }}</span>
        </p>
      </template>
    </div>

    <div class="section">
      <div class="eyebrow">{{ t('task.widgets.preValidationPanel.runHistory') }}</div>
      <p v-if="validation.runs.length === 0" class="muted">{{ t('task.widgets.preValidationPanel.noRuns') }}</p>
      <div v-else class="card list">
        <div
          v-for="(r, i) in validation.runs"
          :key="r.run_id"
          class="row"
          :class="{ first: i === 0 }"
        >
          <div class="row-main">
            <div class="row-title">{{ t('task.widgets.preValidationPanel.runOn', { when: r.ran_at }) }}</div>
            <div class="row-sub">
              <span class="mono ok">{{ r.passed }} {{ t('task.widgets.preValidationPanel.pass') }}</span>
              · <span class="mono warn">{{ r.failed }} {{ t('task.widgets.preValidationPanel.fail') }}</span>
              · <span class="mono muted">{{ r.warnings }} {{ t('task.widgets.preValidationPanel.warn') }}</span>
            </div>
            <details class="tech">
              <summary>{{ t('common.technicalDetails') }}</summary>
              <span class="mono">{{ r.run_id }}</span>
              <span class="row-sep">{{ r.spec_version }}</span>
            </details>
          </div>
          <Chip :kind="r.result === 'pass' ? 'ok' : 'danger'">{{ r.result }}</Chip>
        </div>
      </div>
    </div>
  </template>
</template>

<style scoped>
.section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.eyebrow {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--faint);
}
.status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.muted { color: var(--muted); font-size: 13px; }
.mono { font-family: var(--font-mono); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.list { overflow: hidden; }
.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }
.row-main { flex: 1; min-width: 0; }
.row-title { font-weight: 600; display: inline-flex; align-items: center; gap: 8px; }
.row-sep { color: var(--faint); font-weight: 400; font-size: 12.5px; }
.row-sub { color: var(--muted); font-size: 12.5px; margin-top: 4px; }
.row-sub .ok { color: var(--ok); }
.row-sub .warn { color: var(--danger); }

.btn {
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
.btn:disabled { opacity: 0.6; cursor: default; }
.btn.block { width: 100%; }
.btn-primary { background: var(--accent); color: var(--accent-contrast); }
.btn-primary:hover:not(:disabled) { background: var(--accent-deep); }
.error-text { color: var(--danger); font-size: 13px; }

.tech { margin-top: 4px; }
.tech summary {
  cursor: pointer;
  color: var(--faint);
  font-size: 12px;
  user-select: none;
}
.tech summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.tech[open] summary { margin-bottom: 6px; }
.tech span + span { margin-left: 8px; }
</style>
