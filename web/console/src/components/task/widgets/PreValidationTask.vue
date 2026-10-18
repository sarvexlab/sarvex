<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Sarvex's own pre-validation suite — bespoke rendering for the journey's
// validation:<store>:run milestone (raised on entering Ready, resolved the
// moment any run completes, pass or fail — see
// internal/planes/control/validation/orchestrator.go's RunValidation) and
// its failure counterpart validation:<store>:pre_validation_failed (raised
// when a run fails, retired the next time one passes). Both get the same
// card: same underlying suite, same action, just a different framing while
// nothing has run yet vs. after a failure.
//
// Deliberately scoped to these two refs (see registry.ts): External
// sandbox/production verdicts are also source=validation, and this card
// would be a lie for either of those.
//
// The actual "run it" action lives in the drawer (PreValidationPanel.vue),
// not here — this card is a status preview + click-to-open, same as every
// other milestone widget.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import { useValidationStore } from '@/stores/validation'
import { formatFriendlyDateTime } from '@/lib/datetime'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import TaskCardShell from '../TaskCardShell.vue'
import { useWidgetData } from './useWidgetData'

type Task = components['schemas']['task']

const props = defineProps<{ task: Task }>()
const emit = defineEmits<{ open: [task: Task] }>()

const validation = useValidationStore()
const { t } = useI18n()

useWidgetData(async () => {
  if (!validation.loaded && !validation.loading) await validation.load()
})

const lastRun = computed(() => validation.status?.last_run_at ?? null)
const status = computed(() => validation.status?.pre_validation ?? 'not_run')
const passed = computed(() => status.value === 'passing')
const failed = computed(() => status.value === 'failing')

const title = computed(() => {
  if (passed.value) return t('task.widgets.preValidationTask.passedTitle')
  if (failed.value) return props.task.title ?? t('task.widgets.preValidationTask.failedTitleDefault')
  return props.task.title ?? t('task.widgets.preValidationTask.runTitleDefault')
})
const why = computed(() => (passed.value ? undefined : props.task.why_it_matters))
</script>

<template>
  <TaskCardShell
    :owner="task.owner!"
    :state="task.state!"
    :title="title"
    :why="why"
    @activate="emit('open', task)"
  >
    <template #meta>
      <span v-if="lastRun">{{ t('task.widgets.preValidationTask.lastRun', { when: formatFriendlyDateTime(lastRun) }) }}</span>
      <span v-else>{{ t('task.widgets.preValidationTask.notRunYet') }}</span>
    </template>

    <template #side>
      <Chip v-if="passed" kind="ok">{{ t('task.widgets.preValidationTask.passed') }}</Chip>
      <Chip v-else-if="failed" kind="danger">{{ t('task.widgets.preValidationTask.failed') }}</Chip>
      <Chip v-else kind="quiet"><Icon name="zap" /> {{ t('task.widgets.preValidationTask.runIt') }}</Chip>
    </template>
  </TaskCardShell>
</template>
