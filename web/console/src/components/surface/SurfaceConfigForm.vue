<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Renders a surface's merchant-editable settings from the descriptor's
// `config_schema`. Like the rest of the enrollment flow it has no knowledge
// of any particular surface: field kinds are the schema's (`text`, `bool`,
// `string_list`, `json`), labels are the descriptor's i18n keys, and values
// travel as the strings the API stores.

import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSurfacesStore } from '@/stores/surfaces'
import LoadingButton from '@/components/ui/LoadingButton.vue'
import type { SurfaceEnrollment } from '@/api/surfaces'

const props = defineProps<{ enrollment: SurfaceEnrollment }>()

const surfaces = useSurfacesStore()
const { t, te } = useI18n()

const schema = computed(() => props.enrollment.descriptor?.config_schema ?? [])
const surfaceId = computed(() => props.enrollment.surface_id)
const busy = computed(() => surfaces.isBusy(surfaceId.value))

function label(key: string | undefined, fallback = ''): string {
  if (!key) return fallback
  return te(key) ? t(key) : (key.split('.').pop() ?? fallback)
}

// Local edit buffer, reset whenever the server's config changes under us.
const draft = reactive<Record<string, string>>({})
watch(
  () => props.enrollment.config,
  (config) => {
    for (const f of schema.value) draft[f.key] = config?.[f.key] ?? ''
  },
  { immediate: true, deep: true },
)

function jsonError(key: string, type: string): boolean {
  const v = draft[key]
  if (type !== 'json' || !v || !v.trim()) return false
  try {
    JSON.parse(v)
    return false
  } catch {
    return true
  }
}

const invalid = computed(() => schema.value.some((f) => jsonError(f.key, f.type)))

function onSave() {
  const out: Record<string, string> = {}
  for (const f of schema.value) {
    const v = draft[f.key]
    if (f.type === 'bool') out[f.key] = v === 'true' ? 'true' : 'false'
    else if (v && v.trim()) out[f.key] = v
  }
  surfaces.saveConfig(surfaceId.value, out)
}
</script>

<template>
  <form v-if="schema.length" class="config" @submit.prevent="onSave">
    <div v-for="f in schema" :key="f.key" class="field">
      <label v-if="f.type === 'bool'" class="check">
        <input
          type="checkbox"
          :checked="draft[f.key] === 'true'"
          @change="draft[f.key] = ($event.target as HTMLInputElement).checked ? 'true' : 'false'"
        />
        <span>{{ label(f.label_key, f.key) }}</span>
      </label>
      <template v-else>
        <label :for="`cfg-${surfaceId}-${f.key}`" class="field-label">
          {{ label(f.label_key, f.key) }}
        </label>
        <input
          v-if="f.type === 'text'"
          :id="`cfg-${surfaceId}-${f.key}`"
          v-model="draft[f.key]"
          type="text"
          class="input"
        />
        <textarea
          v-else
          :id="`cfg-${surfaceId}-${f.key}`"
          v-model="draft[f.key]"
          class="input area"
          :class="{ bad: jsonError(f.key, f.type) }"
          rows="4"
          spellcheck="false"
        />
        <div v-if="f.type === 'string_list'" class="hint">{{ t('surfaces.config.listHint') }}</div>
        <div v-if="jsonError(f.key, f.type)" class="error">{{ t('surfaces.config.jsonInvalid') }}</div>
      </template>
      <div v-if="f.help_key" class="hint">{{ label(f.help_key) }}</div>
    </div>
    <LoadingButton type="submit" class="action" :loading="busy" :disabled="invalid">
      {{ t('surfaces.action.save') }}
    </LoadingButton>
  </form>
</template>

<style scoped>
.config {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.field-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}
.check {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 13.5px;
  color: var(--ink);
}
.input {
  font: inherit;
  font-size: 13px;
  color: var(--ink);
  background: var(--surface);
  border: 0;
  border-radius: var(--radius-sm);
  box-shadow: inset 0 0 0 1px var(--line2);
  padding: 8px 10px;
}
.input:focus {
  outline: none;
  box-shadow: inset 0 0 0 1.5px var(--accent-deep);
}
.input.bad {
  box-shadow: inset 0 0 0 1.5px var(--danger);
}
.area {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  resize: vertical;
}
.hint {
  font-size: 12px;
  color: var(--faint);
}
.error {
  font-size: 12px;
  color: var(--danger);
}
.action {
  align-self: flex-start;
}
</style>
