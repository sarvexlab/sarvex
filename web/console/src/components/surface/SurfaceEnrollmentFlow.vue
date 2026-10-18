<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The one component that renders every surface's enrollment flow.
//
// It is generic by construction: everything it draws comes from the
// descriptor on the enrollment record — prerequisite checks, artifacts,
// external steps, verification status — and there is no branch on a
// surface id anywhere in this file. That is the whole point of R20: adding
// a surface must be a descriptor plus its backend adapters, with no new
// frontend code at all.
//
// If you find yourself wanting `v-if="surface_id === 'example_marketplace'"` here,
// the descriptor is missing a field. Add it there.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSurfacesStore } from '@/stores/surfaces'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import CopyField from '@/components/ui/CopyField.vue'
import LoadingButton from '@/components/ui/LoadingButton.vue'
import SurfaceConfigForm from './SurfaceConfigForm.vue'
import type { SurfaceEnrollment } from '@/api/surfaces'

const props = defineProps<{ enrollment: SurfaceEnrollment }>()

const surfaces = useSurfacesStore()
const { t, te } = useI18n()

const descriptor = computed(() => props.enrollment.descriptor)
const state = computed(() => props.enrollment.state)
const surfaceId = computed(() => props.enrollment.surface_id)
const busy = computed(() => surfaces.isBusy(surfaceId.value))

/** Descriptor label keys are i18n keys, but a surface added by the backend
 * may land before its copy does. Falling back to the key's last segment
 * keeps a new surface legible instead of rendering a raw dotted key —
 * which is what makes "register a descriptor, ship nothing else" true in
 * practice and not just in principle. */
function label(key: string | undefined, fallback = ''): string {
  if (!key) return fallback
  return te(key) ? t(key) : (key.split('.').pop() ?? fallback)
}

/** First of several i18n keys that has copy, else a humanised fallback.
 * Lets a surface's own folder (`surfaces.<id>.…`) override the generic
 * `surfaces.…` wording without this component naming any surface. */
function firstLabel(keys: string[], raw: string): string {
  const hit = keys.find((k) => te(k))
  if (hit) return t(hit)
  const words = raw.replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

const hasConfig = computed(() => (descriptor.value?.config_schema?.length ?? 0) > 0)

const isEnrolled = computed(() => state.value === 'enrolled')
const isAvailable = computed(() => state.value === 'available')
const hasStarted = computed(() => !isAvailable.value)

/** Prerequisites only mean something once enrollment has started. */
const prerequisites = computed(() => (hasStarted.value ? (props.enrollment.prerequisites ?? []) : []))
const failing = computed(() => prerequisites.value.filter((p) => !p.passed))
const allPassed = computed(
  () => prerequisites.value.length > 0 && failing.value.length === 0,
)

const artifacts = computed(() => props.enrollment.artifacts ?? [])
const externalSteps = computed(() => descriptor.value?.external_steps ?? [])

/** External steps are actionable exactly when the prerequisites are done
 * and the merchant has not yet told us they finished them. */
const awaitingSubmission = computed(
  () => state.value === 'configured' && externalSteps.value.length > 0,
)

const isBlocked = computed(() => props.enrollment.blocked === true)
</script>

<template>
  <div class="flow">
    <!-- Blocked: the store is not yet UCP active. The one cross-lifecycle
         rule, and the only reason a surface can't be started. -->
    <div v-if="isBlocked" class="note note-blocked">
      <Icon name="lock" :size="14" />
      <span>{{ label(enrollment.blocked_reason, t('surfaces.blocked.storeNotActive')) }}</span>
    </div>

    <!-- Not started: describe what enrolling involves, from the descriptor. -->
    <template v-else-if="isAvailable">
      <ul v-if="descriptor?.prerequisite_checks?.length" class="preview">
        <li v-for="check in descriptor.prerequisite_checks" :key="check">
          <Icon name="note" :size="12" />
          {{ firstLabel([`surfaces.${surfaceId}.check.${check}`, `surfaces.prereq.${check}`], check) }}
        </li>
      </ul>
      <LoadingButton class="action" :loading="busy" @click="surfaces.enroll(surfaceId)">
        {{ t('surfaces.action.enroll') }}
      </LoadingButton>
    </template>

    <template v-else>
      <!-- Prerequisites: pass/fail with a merchant-readable reason and,
           where one exists, a remediation. -->
      <section v-if="prerequisites.length" class="block">
        <div class="block-label">{{ t('surfaces.section.prerequisites') }}</div>
        <ul class="prereqs">
          <li v-for="p in prerequisites" :key="p.check_id" :class="{ failed: !p.passed }">
            <Icon :name="p.passed ? 'check' : 'x'" :size="14" />
            <div class="prereq-body">
              <div class="prereq-title">{{ label(p.label_key, p.check_id) }}</div>
              <div v-if="!p.passed && p.reason" class="prereq-reason">{{ p.reason }}</div>
              <a
                v-if="!p.passed && p.remediation_url"
                class="prereq-fix"
                :href="p.remediation_url"
                target="_blank"
                rel="noopener"
              >
                {{ label(p.remediation_key, t('surfaces.action.fix')) }}
              </a>
            </div>
          </li>
        </ul>
        <button
          type="button"
          class="btn-ghost"
          :disabled="busy"
          @click="surfaces.evaluate(surfaceId)"
        >
          <Icon name="zap" :size="12" /> {{ t('surfaces.action.recheck') }}
        </button>
      </section>

      <!-- Artifacts: Sarvex-generated and surface-scoped. Rendered from the
           record, so a surface with none shows nothing here. -->
      <section v-if="artifacts.length" class="block">
        <div class="block-label">{{ t('surfaces.section.artifacts') }}</div>
        <div v-for="a in artifacts" :key="a.artifact_id" class="artifact">
          <CopyField
            v-if="a.url"
            :label="label(a.label_key, a.artifact_id)"
            :value="a.url"
          />
          <dl v-if="a.detail" class="artifact-detail">
            <template v-for="(value, key) in a.detail" :key="key">
              <dt>
                {{
                  firstLabel(
                    [`surfaces.${surfaceId}.artifactDetail.${key}`, `surfaces.artifactDetail.${key}`],
                    String(key),
                  )
                }}
              </dt>
              <dd>{{ value }}</dd>
            </template>
          </dl>
        </div>
      </section>

      <!-- Settings: rendered from the descriptor's config_schema. -->
      <section v-if="hasConfig" class="block">
        <div class="block-label">{{ t('surfaces.section.config') }}</div>
        <SurfaceConfigForm :enrollment="enrollment" />
      </section>

      <!-- External steps: things the merchant does outside Sarvex. -->
      <section v-if="externalSteps.length && !isEnrolled" class="block">
        <div class="block-label">{{ t('surfaces.section.externalSteps') }}</div>
        <ol class="steps">
          <li v-for="step in externalSteps" :key="step.step_id">
            <div class="step-body">
              <span>{{ label(step.label_key, step.step_id) }}</span>
              <Chip :kind="step.owner === 'merchant' ? 'you' : 'external'">
                {{
                  step.owner === 'merchant'
                    ? t('surfaces.stepOwner.merchant')
                    : label(descriptor?.display_name_key, surfaceId)
                }}
              </Chip>
            </div>
            <a v-if="step.url" :href="step.url" target="_blank" rel="noopener" class="step-link">
              {{ t('surfaces.action.open') }}
            </a>
          </li>
        </ol>
        <LoadingButton
          v-if="awaitingSubmission && allPassed"
          class="action"
          :loading="busy"
          @click="surfaces.submit(surfaceId)"
        >
          {{ t('surfaces.action.markStepsDone') }}
        </LoadingButton>
      </section>

      <!-- Verification status. -->
      <div v-if="state === 'verifying'" class="note">
        <Icon name="clock" :size="14" />
        <span>
          {{
            t('surfaces.verifying', {
              signal: firstLabel(
                [
                  `surfaces.${surfaceId}.signal.${descriptor?.verification_signal}`,
                  `surfaces.signal.${descriptor?.verification_signal}`,
                ],
                descriptor?.verification_signal ?? '',
              ),
            })
          }}
        </span>
      </div>

      <div v-if="isEnrolled" class="note note-ok">
        <Icon name="check" :size="14" />
        <span>{{ t('surfaces.enrolled') }}</span>
      </div>

      <div v-if="state === 'suspended'" class="note note-danger">
        <Icon name="shield" :size="14" />
        <span>{{ enrollment.suspension_reason || t('surfaces.suspended') }}</span>
      </div>

      <button
        type="button"
        class="btn-ghost withdraw"
        :disabled="busy"
        @click="surfaces.withdraw(surfaceId)"
      >
        {{ t('surfaces.action.withdraw') }}
      </button>
    </template>
  </div>
</template>

<style scoped>
.flow {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.block {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.block-label {
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--faint);
}

.preview {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 13px;
}
.preview li {
  display: flex;
  align-items: center;
  gap: 8px;
}

.prereqs {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.prereqs li {
  display: flex;
  gap: 9px;
  align-items: flex-start;
  color: var(--ok, var(--muted));
}
.prereqs li.failed {
  color: var(--danger);
}
.prereq-body {
  flex: 1;
  min-width: 0;
}
.prereq-title {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--ink);
}
.prereq-reason {
  font-size: 12.5px;
  color: var(--muted);
  margin-top: 3px;
}
.prereq-fix {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--accent-deep);
  margin-top: 4px;
}
.prereq-fix:hover {
  text-decoration: underline;
}

.artifact {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.artifact-detail {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px 14px;
  margin: 0;
  font-size: 12.5px;
}
.artifact-detail dt {
  color: var(--faint);
}
.artifact-detail dd {
  margin: 0;
  color: var(--ink);
}

.steps {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 13.5px;
}
.step-body {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
}
.step-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--accent-deep);
  margin-top: 3px;
}

.note {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--muted);
  background: var(--surface2);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
}
.note-ok {
  color: var(--ok);
}
.note-danger {
  color: var(--danger);
}
.note-blocked {
  color: var(--faint);
}

/* LoadingButton carries the house .btn/.btn-primary styling; this only
   places it. */
.action {
  align-self: flex-start;
}

.btn-ghost {
  align-self: flex-start;
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
  border: 0;
  border-radius: var(--radius-sm);
  padding: 6px 11px;
  font-weight: 600;
  font-size: 12px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.btn-ghost:hover:not(:disabled) {
  box-shadow: inset 0 0 0 1px var(--faint);
}
.btn-ghost:disabled {
  opacity: 0.55;
  cursor: default;
}
.withdraw {
  color: var(--muted);
}
</style>
