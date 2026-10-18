<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The task drill-in. Header (what/why/owner/source) renders immediately from
// the list-page task the caller already has; the body fetches GET
// /tasks/{task_id} for the typed drill-in payload (sarvex-v1-control-plane.yaml
// "Get a task with its typed drill-in payload").
//
// The body renders whichever typed payload actually came back, rather than
// guessing from the task's source:
//
//   - remediations → the rule-based fixes as a diff (from -> to), with a
//     batch Apply and a revert once applied
//   - issues → the fields with no rule-based fix (remediable=manual), as a
//     plain field + problem list, the same shape ProductDrawer uses
//   - finding → the consistency finding that raised the task, read-only and
//     snapshotted at raise time rather than re-probed, so it reflects what
//     was actually observed
//   - nothing → a line addressed to whoever owns the task
//
// It branched on source before, which is a statement about which domain
// emitted a task, not about what is known about it. That put the readiness
// *milestone* — "Auditing your catalog", owned by Sarvex — into the per-SKU
// readiness branch, telling the merchant to go fix it in their store.

import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import { getTask } from '@/api/tasks'
import { applyRemediations, revertWriteBack } from '@/api/remediations'
import { formatFriendlyDateTime } from '@/lib/datetime'
import { problemLabel } from '@/lib/issue'
import OwnerChip from '@/components/ui/OwnerChip.vue'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import PreValidationPanel from '@/components/task/widgets/PreValidationPanel.vue'

type Task = components['schemas']['task']

const props = defineProps<{ task: Task }>()

const { t } = useI18n()

const emit = defineEmits<{
  close: []
  resolve: [task: Task]
}>()

const detail = ref<Task | null>(null)
const detailLoading = ref(false)
const detailError = ref<string | null>(null)

const applying = ref(false)
const applyError = ref<string | null>(null)
const applyResult = ref<{ applied: number; write_back_ids: string[] } | null>(null)

const reverting = ref(false)
const reverted = ref(false)

async function loadDetail() {
  if (!props.task.task_id) return
  detailLoading.value = true
  detailError.value = null
  applyResult.value = null
  applyError.value = null
  reverted.value = false
  try {
    detail.value = await getTask(props.task.task_id)
  } catch (e: unknown) {
    detailError.value = e instanceof Error ? e.message : t('task.drawer.errors.loadDetail')
  } finally {
    detailLoading.value = false
  }
}
onMounted(loadDetail)
// Keyed on state as well as id: a live task.updated push hands the drawer a
// new object for the *same* task, and watching the id alone left the typed
// drill-in body showing what the task looked like when it was opened.
watch(() => [props.task.task_id, props.task.state], loadDetail)

// Sarvex's own pre-validation milestone/failure tasks — see registry.ts's
// pre_validation entry for why both refs are matched together.
const isPreValidationTask = computed(
  () =>
    props.task.source === 'validation' &&
    (props.task.source_ref === 'run' || props.task.source_ref === 'pre_validation_failed'),
)

async function onApply() {
  if (!detail.value?.remediations?.length) return
  applying.value = true
  applyError.value = null
  try {
    applyResult.value = await applyRemediations(
      props.task.store_id ?? '',
      detail.value.remediations.map((r) => r.remediation_id).filter((id): id is string => !!id),
    )
  } catch (e: unknown) {
    applyError.value = e instanceof Error ? e.message : t('task.drawer.errors.apply')
  } finally {
    applying.value = false
  }
}

async function onRevert() {
  const writeBackId = applyResult.value?.write_back_ids[0]
  if (!writeBackId) return
  reverting.value = true
  try {
    await revertWriteBack(props.task.store_id ?? '', writeBackId)
    reverted.value = true
  } catch (e: unknown) {
    applyError.value = e instanceof Error ? e.message : t('task.drawer.errors.revert')
  } finally {
    reverting.value = false
  }
}

type Severity = components['schemas']['severity']
const severityChipKind: Record<Severity, 'danger' | 'warn' | 'quiet'> = {
  critical: 'danger',
  warning: 'warn',
  info: 'quiet',
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

function onResolve() {
  emit('resolve', props.task)
}

/**
 * What to say when the task has no typed drill-in payload.
 *
 * Keyed off owner, not source. Branching on source meant the readiness
 * *milestone* ("Auditing your catalog", owned by Sarvex and in progress)
 * fell into the per-SKU readiness branch and told the merchant "no automatic
 * fix — you'll need to update it directly in your store", directly above a
 * footer saying Sarvex was handling it. Source says which domain emitted a
 * task; it does not say whose problem it is.
 */
const noDetailMessage = computed(() => {
  switch (props.task.owner) {
    case 'sarvex':
      return t('task.drawer.noDetail.sarvex')
    default:
      return t('task.drawer.noDetail.default')
  }
})
</script>

<template>
  <div class="scrim" @click="emit('close')" />
  <aside class="drawer" role="dialog" aria-modal="true">
    <div class="drawer-head">
      <div class="head-top">
        <div class="head-chips">
          <OwnerChip v-if="task.owner" :owner="task.owner" />
          <Chip v-if="task.source" kind="quiet">{{ task.source }}</Chip>
        </div>
        <button class="icon-btn" :aria-label="t('task.drawer.close')" @click="emit('close')">
          <Icon name="x" />
        </button>
      </div>
      <h2 class="display title">{{ task.title }}</h2>
      <p v-if="task.why_it_matters" class="why">{{ task.why_it_matters }}</p>
    </div>

    <div class="drawer-body">
      <div v-if="detailLoading" class="muted-row">
        <span class="dots"><i /><i /><i /></span>
        {{ t('task.drawer.loadingDetails') }}
      </div>

      <div v-else-if="detailError" class="section">
        <p class="muted">{{ detailError }}</p>
        <button class="btn btn-ghost" @click="loadDetail">{{ t('common.error.retry') }}</button>
      </div>

      <template v-else-if="detail?.remediations?.length">
        <div class="section">
          <div class="eyebrow">{{ t('task.drawer.proposedFix') }}</div>
          <div class="card list">
            <div
              v-for="(r, i) in detail.remediations"
              :key="r.remediation_id"
              class="row"
              :class="{ first: i === 0 }"
            >
              <div class="row-main">
                <div class="row-title">
                  <span class="mono">{{ r.field }}</span>
                </div>
                <div class="row-sub diff">
                  <span class="from">{{ r.from_value || t('task.drawer.emptyValue') }}</span>
                  <span class="arrow"><Icon name="arrowR" /></span>
                  <span class="to">{{ r.to_value }}</span>
                </div>
              </div>
              <Chip :kind="r.trust === 'high' ? 'ok' : 'quiet'">
                {{ r.trust === 'high' ? t('task.drawer.highTrust') : t('task.drawer.lowTrust') }}
              </Chip>
            </div>
          </div>
        </div>

        <div v-if="applyResult" class="section">
          <div class="result-banner result-ok">
            <Icon name="check" />
            <span>
              {{ applyResult.applied === 1
                ? t('task.drawer.appliedSingular', { count: applyResult.applied })
                : t('task.drawer.appliedPlural', { count: applyResult.applied }) }}
            </span>
          </div>
          <button
            v-if="!reverted"
            class="btn btn-ghost block"
            :disabled="reverting"
            @click="onRevert"
          >
            <Icon name="swap" />
            {{ reverting ? t('task.drawer.reverting') : t('task.drawer.revert') }}
          </button>
          <p v-else class="muted">{{ t('task.drawer.revertedNote') }}</p>
        </div>

        <div v-else class="section">
          <button class="btn btn-primary block" :disabled="applying" @click="onApply">
            <Icon name="apply" />
            {{ applying
              ? t('task.drawer.applying')
              : (detail.remediations.length > 1
                ? t('task.drawer.applyFixesN', { count: detail.remediations.length })
                : t('task.drawer.applyFix')) }}
          </button>
          <p v-if="applyError" class="error-text">{{ applyError }}</p>
        </div>
      </template>

      <template v-else-if="detail?.issues?.length">
        <div class="section">
          <div class="eyebrow">{{ t('task.drawer.whatToFix') }}</div>
          <div class="card list">
            <div
              v-for="(iss, i) in detail.issues"
              :key="iss.issue_id"
              class="row"
              :class="{ first: i === 0 }"
            >
              <div class="row-main">
                <div class="row-title">
                  <span class="mono">{{ iss.field }}</span>
                  <span class="row-sep">·</span>
                  <span>{{ problemLabel(t, iss.problem) }}</span>
                </div>
              </div>
              <Chip v-if="iss.blocks_checkout" kind="danger">{{ t('task.drawer.blocks') }}</Chip>
            </div>
          </div>
          <p class="muted">
            {{ detail.issues.length > 1 ? t('task.drawer.noAutoFixPlural') : t('task.drawer.noAutoFixSingular') }}
          </p>
        </div>
      </template>

      <template v-else-if="detail?.finding">
        <div class="section">
          <div class="eyebrow">{{ t('task.drawer.finding') }}</div>
          <div class="card list">
            <div class="row first">
              <div class="row-main">
                <div class="row-title">
                  <span class="mono">{{ detail.finding.field }}</span>
                </div>
                <div class="row-sub sources">
                  <span
                    v-for="(value, source) in detail.finding.sources"
                    :key="String(source)"
                    class="src"
                  >
                    <span class="src-label">{{ source }}:</span>
                    <span class="mono">{{ value }}</span>
                  </span>
                </div>
              </div>
              <Chip v-if="detail.finding.severity" :kind="severityChipKind[detail.finding.severity]">
                {{ detail.finding.severity }}
              </Chip>
            </div>
          </div>
          <p v-if="detail.finding.detected_at" class="muted">
            {{ t('task.drawer.detected', { when: formatFriendlyDateTime(detail.finding.detected_at) }) }}
          </p>
        </div>
      </template>

      <PreValidationPanel v-else-if="isPreValidationTask" />

      <p v-else class="muted">{{ noDetailMessage }}</p>
    </div>

    <div class="drawer-foot">
      <button
        v-if="
          task.owner === 'merchant' &&
          task.resolution !== 'automatic' &&
          task.state !== 'done' &&
          !isPreValidationTask
        "
        class="btn btn-primary block"
        @click="onResolve"
      >
        <Icon name="check" />
        {{ task.resolution === 'one_click' && !detail?.remediations?.length ? t('task.drawer.footer.apply') : t('task.drawer.footer.markDone') }}
      </button>
      <div
        v-else-if="task.owner === 'merchant' && task.resolution === 'automatic' && task.state !== 'done'"
        class="muted-row"
      >
        <Icon name="clock" />
        {{ t('task.drawer.footer.verifiesAutomatically') }}
      </div>
      <button v-else-if="isPreValidationTask" class="btn btn-ghost block" @click="emit('close')">
        {{ t('task.drawer.close') }}
      </button>
      <div v-else-if="task.owner === 'sarvex' && task.state !== 'done'" class="muted-row">
        <span class="dots"><i /><i /><i /></span>
        {{ t('task.drawer.footer.sarvexHandling') }}
      </div>
      <button v-else class="btn btn-ghost block" @click="emit('close')">{{ t('task.drawer.close') }}</button>
    </div>
  </aside>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  background: color-mix(in oklab, var(--ink) 32%, transparent);
  backdrop-filter: blur(2px);
  z-index: 60;
  animation: fade 0.2s both;
}
@keyframes fade { from { opacity: 0; } to { opacity: 1; } }

.drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(540px, 96vw);
  background: var(--bg);
  z-index: 61;
  box-shadow: -20px 0 60px -20px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  animation: slidein 0.32s cubic-bezier(0.22, 0.9, 0.3, 1) both;
}
@keyframes slidein { from { transform: translateX(100%); } to { transform: none; } }

.drawer-head {
  padding: 22px 26px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.head-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.head-chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

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

.title { font-size: 23px; margin-top: 14px; }
.why { color: var(--muted); margin-top: 8px; }

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 24px 26px 40px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.muted { color: var(--muted); }

.section .eyebrow {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--faint);
  margin-bottom: 10px;
}
.section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

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
.row-title {
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.row-sub { font-size: 12.5px; color: var(--muted); margin-top: 5px; }
.row-sep { color: var(--faint); font-weight: 400; }
.diff { display: inline-flex; align-items: center; gap: 8px; }
.diff .from { text-decoration: line-through; color: var(--faint); }
.diff .to { color: var(--ink); font-weight: 600; }
.arrow { color: var(--accent); display: inline-flex; }
.arrow :deep(svg) { width: 14px; height: 14px; }

.sources { display: flex; flex-direction: column; gap: 3px; }
.src { display: block; }
.src-label { color: var(--faint); margin-right: 5px; }

.result-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-radius: var(--radius-sm);
  font-size: 13.5px;
  font-weight: 600;
}
.result-ok {
  background: color-mix(in oklab, var(--ok) 14%, transparent);
  color: var(--ok);
}
.error-text { color: var(--danger); font-size: 13px; }

.drawer-foot {
  padding: 16px 26px;
  border-top: 1px solid var(--line);
  background: var(--surface);
  display: flex;
  gap: 10px;
}
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
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover { background: var(--accent-deep); }
.btn-ghost {
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
}
.btn-ghost:hover { box-shadow: inset 0 0 0 1px var(--faint); }

.muted-row {
  color: var(--muted);
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px;
}
.dots {
  display: inline-flex;
  gap: 3px;
  align-items: center;
}
.dots i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--muted);
  display: block;
}
@media (prefers-reduced-motion: no-preference) {
  .dots i { animation: pd 1.2s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: 0.15s; }
  .dots i:nth-child(3) { animation-delay: 0.3s; }
  @keyframes pd { 0%, 100% { opacity: 0.25; } 50% { opacity: 1; } }
}
</style>
