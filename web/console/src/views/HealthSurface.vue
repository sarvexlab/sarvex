<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Post-go-live reassurance surface. The full statements list activates only
// once the journey reaches 'live' — before that it's an explainer. Anything
// actionable here also appears as a task on the Path (link, don't duplicate).
//
// Endpoints:
//   GET /stores/{id}/health                → statements + endpoint_status + active_alerts
//   GET /stores/{id}/health/consistency    → consistency findings (drill-in)

import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useHealthStore } from '@/stores/health'
import { useStoresStore } from '@/stores/stores'
import { formatFriendlyDateTime } from '@/lib/datetime'
import { useNow } from '@/lib/now'
import {
  ageSeconds,
  checkKey,
  countParam,
  formatApproximateAge,
  isFailingState,
  isSystemFinding,
  sortChecks,
  systemFindingSubject,
} from '@/lib/health'
import type { components } from '@/api/schema'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

type HealthStatement = components['schemas']['health_statement']

const health = useHealthStore()
const stores = useStoresStore()
const { t } = useI18n()

// Relative times must keep moving. formatFriendlyDateTime renders "2 minutes
// ago" from a fixed input, so calling it once produced a phrase that stayed
// put for as long as the page was open — every timestamp claiming the page
// had just loaded, however long ago it had. Reading the ticking clock inside
// the call is what makes Vue re-render the phrase.
const now = useNow()
function relative(iso: string | null | undefined): string {
  void now.value
  return formatFriendlyDateTime(iso)
}

const store = computed(() => stores.currentStore)
// Health is meaningful once the store's own endpoints are live and
// monitored — an activation fact. Keyed on journey_state === 'live' this
// stayed an explainer forever for any store that never enrolled with
// Google, however healthy its endpoints actually were.
const isLive = computed(() => store.value?.ucp_state === 'active')
const statements = computed(() => health.health?.statements ?? [])
const endpointStatus = computed(() => health.health?.endpoint_status ?? null)
const activeAlerts = computed(() => health.health?.active_alerts ?? [])

const checkEntries = computed(() => sortChecks(Object.entries(endpointStatus.value?.checks ?? {})))

function statusChipKind(status: string | undefined) {
  if (status === 'critical') return 'danger'
  if (status === 'degraded') return 'warn'
  return 'ok'
}

function checkChipKind(state: string | undefined) {
  if (state === 'fail') return 'danger'
  if (state === 'degraded' || state === 'timeout') return 'warn'
  return 'ok'
}

function checkLabel(checkType: string): string {
  const key = checkKey(checkType)
  return key ? t(`health.checks.${key}.label`) : checkType
}

function checkMeaning(checkType: string): string | null {
  const key = checkKey(checkType)
  return key ? t(`health.checks.${key}.meaning`) : null
}

function checkStateLabel(state: string | undefined): string {
  if (!state) return ''
  const known = ['pass', 'degraded', 'fail', 'timeout']
  return known.includes(state) ? t(`health.checkState.${state}`) : state
}

// "degraded" alone tells the merchant nothing. The reason is always which
// checks are currently not passing, so name them.
const statusReason = computed(() => {
  const status = endpointStatus.value?.status
  if (!status) return null
  const failing = checkEntries.value
    .filter(([, s]) => isFailingState((s as { state?: string }).state))
    .map(([type]) => checkLabel(type))

  if (status === 'healthy' || failing.length === 0) return t('health.statusReason.healthy')
  const checks = failing.join(', ')
  const plural = failing.length > 1
  if (status === 'critical') {
    return t(plural ? 'health.statusReason.criticalPlural' : 'health.statusReason.critical', { checks })
  }
  return t(plural ? 'health.statusReason.degradedPlural' : 'health.statusReason.degraded', { checks })
})

/** Console-rendered statement copy, falling back to the server's sentence for
 *  any kind we have no copy for (a failing semantic_endpoint carries detail in
 *  its message that we cannot reconstruct). */
function statementText(s: HealthStatement): string {
  const params = (s.params ?? {}) as Record<string, unknown>
  if (s.kind === 'consistency') {
    const critical = countParam(params, 'critical_count')
    const warning = countParam(params, 'warning_count')
    if (critical > 0) return t('health.statements.consistency.critical', { count: critical }, critical)
    if (warning > 0) return t('health.statements.consistency.warning', { count: warning }, warning)
    return t('health.statements.consistency.ok')
  }
  if (s.kind === 'sync_freshness') {
    if (s.ok) return t('health.statements.sync_freshness.ok')
    const age = formatApproximateAge(ageSeconds(params))
    if (age) return t('health.statements.sync_freshness.stale', { age })
  }
  return s.message ?? ''
}

/** The "why did this happen" line, where we can explain it. */
function statementWhy(s: HealthStatement): string | null {
  if (s.kind === 'sync_freshness' && !s.ok) return t('health.statements.sync_freshness.staleWhy')
  return null
}

// Findings are keyed by SKU except the plugin-gateway probe's, which are
// about the integration itself.
function findingSubject(f: { sku?: string }): string {
  if (!isSystemFinding(f.sku)) return f.sku ?? ''
  const subject = systemFindingSubject(f.sku)
  const known = ['plugin_gateway']
  return known.includes(subject)
    ? t(`health.findingSystem.${subject}`)
    : t('health.findingSystem.unknown')
}

function findingPredicate(f: { field?: string }): string {
  const known = ['server_health', 'price', 'stock', 'availability']
  return f.field && known.includes(f.field)
    ? t(`health.findingField.${f.field}`)
    : t('health.findingFieldFallback', { field: f.field ?? '' })
}

function isPending(alertId: string) {
  return !!health.alertActionPending[alertId]
}

async function refresh() {
  await health.load()
}
onMounted(refresh)
watch(() => stores.currentStoreId, refresh)

const showSkeleton = computed(() => !health.loaded && health.loading)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">{{ t('health.eyebrow', { storeName: store?.name ?? '' }) }}</div>
      <h1 v-if="isLive" class="page-title display">{{ t('health.liveTitle') }}</h1>
      <h1 v-else class="page-title display">{{ t('health.preLiveTitle') }}</h1>
      <p v-if="isLive" class="page-sub">
        {{ t('health.liveSub') }}
      </p>
      <p v-else class="page-sub">
        {{ t('health.preLiveSub', { storeName: store?.name ?? t('health.storeNameFallback') }) }}
      </p>
    </div>

    <ErrorBanner v-if="health.error" :message="health.error" @retry="refresh" />

    <div v-if="showSkeleton" class="skel-block">
      <Skeleton height="180px" radius="11px" />
    </div>

    <div v-else-if="!isLive" class="card empty-note">
      <div class="big">{{ t('health.nothingToWatchTitle') }}</div>
      {{ store?.name ?? t('health.storeThisFallback') }} {{ t('health.nothingToWatchBody') }}
    </div>

    <template v-else-if="!showSkeleton">
      <div v-if="endpointStatus" class="endpoint-status">
        <div class="card status-head">
          <div class="status-head-main">
            <div :class="['status-dot', endpointStatus.status]" />
            <div>
              <div class="status-label">
                {{ t('health.endpointHealth') }}
                <span class="status-word">{{ endpointStatus.status }}</span>
              </div>
              <div v-if="statusReason" class="status-reason">{{ statusReason }}</div>
              <div class="row-sub">
                {{ t('health.evaluated', { when: relative(endpointStatus.evaluated_at) }) }}
              </div>
            </div>
          </div>
          <Chip :kind="statusChipKind(endpointStatus.status)">{{ endpointStatus.status }}</Chip>
        </div>

        <p v-if="endpointStatus.status !== 'healthy'" class="no-action">
          {{ t('health.noActionNeeded') }}
        </p>

        <div v-if="checkEntries.length > 0" class="card list checks-grid">
          <div
            v-for="([checkType, state], i) in checkEntries"
            :key="checkType"
            class="row"
            :class="{ first: i === 0 }"
          >
            <div class="row-main">
              <div class="row-message">{{ checkLabel(checkType) }}</div>
              <div v-if="checkMeaning(checkType)" class="check-meaning">{{ checkMeaning(checkType) }}</div>
              <div class="row-sub">
                <template v-if="state.last_pass_at">
                  {{ t('health.lastPass', { when: relative(state.last_pass_at) }) }}
                </template>
                <template v-if="state.last_fail_at">
                  · {{ t('health.lastFail', { when: relative(state.last_fail_at) }) }}
                </template>
              </div>
            </div>
            <Chip :kind="checkChipKind(state.state)">{{ checkStateLabel(state.state) }}</Chip>
          </div>
        </div>

        <div v-if="activeAlerts.length > 0" class="card list alerts-list">
          <div
            v-for="(a, i) in activeAlerts"
            :key="a.alert_id"
            class="row alert-row"
            :class="{ first: i === 0 }"
          >
            <div class="row-main">
              <div class="row-message">{{ a.detail }}</div>
              <div class="row-sub">
                {{ checkLabel(a.check_type ?? '') }} ·
                {{ t('health.openedLabel', { when: relative(a.opened_at) }) }}
              </div>
            </div>
            <Chip :kind="statusChipKind(a.severity)">{{ a.severity }}</Chip>
            <div class="alert-actions">
              <button
                v-if="a.status === 'open'"
                class="btn-ghost"
                :disabled="isPending(a.alert_id!)"
                @click="health.acknowledge(a.alert_id!)"
              >
                {{ t('health.acknowledge') }}
              </button>
              <button
                class="btn-ghost"
                :disabled="isPending(a.alert_id!)"
                @click="health.resolve(a.alert_id!)"
              >
                {{ t('health.resolve') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="statements.length === 0" class="card empty-note">
        <div class="big">{{ t('health.noStatementsTitle') }}</div>
        {{ t('health.noStatementsBody') }}
      </div>

      <div v-else class="card list">
        <div
          v-for="(s, i) in statements"
          :key="(s.kind ?? '') + i"
          class="row"
          :class="{ first: i === 0 }"
        >
          <div :class="['hic', s.ok ? 'ok' : 'warn']">
            <Icon :name="s.ok ? 'check' : 'clock'" />
          </div>
          <div class="row-main">
            <div class="row-message">{{ statementText(s) }}</div>
            <div v-if="statementWhy(s)" class="check-meaning">{{ statementWhy(s) }}</div>
            <div v-if="s.checked_at" class="row-sub">
              {{ t('health.checkedLabel', { when: relative(s.checked_at) }) }}
            </div>
          </div>
          <Chip :kind="s.ok ? 'ok' : 'warn'">{{ s.ok ? t('health.ok') : t('health.attention') }}</Chip>
        </div>
      </div>

      <div v-if="health.findings.length > 0" class="findings">
        <h2 class="display section">{{ t('health.consistencyFindings') }}</h2>
        <p class="findings-intro">{{ t('health.findingsIntro') }}</p>
        <div class="card list">
          <div
            v-for="(f, i) in health.findings"
            :key="f.finding_id"
            class="row"
            :class="{ first: i === 0 }"
          >
            <div class="row-main">
              <div class="row-message">
                <span :class="{ mono: !isSystemFinding(f.sku) }">{{ findingSubject(f) }}</span>
                {{ findingPredicate(f) }}
              </div>
              <div class="row-sub">
                <template v-if="isSystemFinding(f.sku) && f.sources?.status">
                  {{ t('health.findingStatus', { status: f.sources.status }) }}
                </template>
                <template v-else>
                  <span v-for="(value, source) in f.sources" :key="String(source)" class="src">
                    <span class="src-label">{{ source }}:</span>
                    <span class="mono">{{ value }}</span>
                  </span>
                </template>
                <template v-if="f.detected_at">
                  · {{ t('health.checkedLabel', { when: relative(f.detected_at) }) }}
                </template>
              </div>
            </div>
            <Chip
              :kind="f.severity === 'critical' ? 'danger' : f.severity === 'warning' ? 'warn' : 'quiet'"
            >
              {{ f.severity }}
            </Chip>
          </div>
        </div>
      </div>

      <p class="pace">
        <span class="dots"><i /><i /><i /></span>
        {{ t('health.continuouslyMonitored') }}
      </p>
    </template>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.empty-note {
  text-align: center;
  padding: 40px 20px;
  color: var(--muted);
}
.empty-note .big {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 22px;
  color: var(--ink);
  margin-bottom: 8px;
  letter-spacing: var(--display-tracking);
}

.list { overflow: hidden; }
.row {
  display: flex;
  align-items: center;
  gap: 15px;
  padding: 18px 22px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }

.hic {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.hic.ok {
  background: color-mix(in oklab, var(--ok) 13%, var(--surface));
  color: var(--ok);
}
.hic.warn {
  background: color-mix(in oklab, var(--warn) 13%, var(--surface));
  color: var(--warn);
}
.hic :deep(svg) { width: 16px; height: 16px; }

.row-main { flex: 1; min-width: 0; }
.row-message { font-weight: 500; }
.row-sub {
  color: var(--faint);
  font-size: 12.5px;
  margin-top: 4px;
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
}
.src { display: inline-flex; gap: 5px; }
.src-label { color: var(--muted); }

.section { font-size: 18px; margin: 30px 0 14px; }
.findings { margin-top: 24px; }

.endpoint-status { margin-bottom: 24px; display: flex; flex-direction: column; gap: 16px; }
.status-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 22px;
}
.status-head-main { display: flex; align-items: center; gap: 14px; }
.status-label { font-weight: 500; }
/* Why the status is what it is — the line that turns "degraded" from a word
   into information. */
.status-reason { color: var(--ink); font-size: 13px; margin-top: 3px; }
/* Plain-language explanation under a check or statement. */
.check-meaning { color: var(--muted); font-size: 12.5px; margin-top: 4px; line-height: 1.45; }
.no-action { color: var(--muted); font-size: 12.5px; margin: 8px 2px 0; }
.findings-intro { color: var(--muted); font-size: 13px; margin: 0 2px 10px; }
.status-word { text-transform: capitalize; }
.status-dot { width: 12px; height: 12px; border-radius: 50%; flex-shrink: 0; }
.status-dot.healthy { background: var(--ok); }
.status-dot.degraded { background: var(--warn); }
.status-dot.critical { background: var(--danger, #d33); }

.checks-grid .row-message { text-transform: none; }
.alert-row { gap: 10px; }
.alert-actions { display: flex; gap: 8px; flex-shrink: 0; }
.btn-ghost {
  border: 1px solid var(--line);
  background: transparent;
  color: var(--ink);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
.btn-ghost:hover:not(:disabled) { background: var(--surface-hover, rgba(0, 0, 0, 0.04)); }
.btn-ghost:disabled { opacity: 0.5; cursor: default; }

.pace {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
  margin-top: 20px;
  color: var(--faint);
  font-size: 13px;
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

.skel-block { display: flex; flex-direction: column; gap: 14px; }
</style>
