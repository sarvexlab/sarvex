<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Label + input + optional hint. The slot is the control so Field stays
// agnostic of whether it's wrapping <input>, <textarea>, <select>, or a
// custom widget. Use Field everywhere a labelled control is needed — keeps
// the form vocabulary uniform across Settings and the drawers.

defineProps<{ label: string; hint?: string }>()
</script>

<template>
  <label class="field">
    <span class="field-label">{{ label }}</span>
    <slot />
    <span v-if="hint" class="field-hint">{{ hint }}</span>
  </label>
</template>

<style scoped>
.field { display: block; margin-bottom: 18px; }
.field:last-child { margin-bottom: 0; }

.field-label {
  display: block;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--muted);
  margin-bottom: 7px;
  letter-spacing: 0.01em;
}

.field :deep(input),
.field :deep(textarea),
.field :deep(select) {
  width: 100%;
  padding: 11px 14px;
  font-size: 14px;
  border: 1px solid var(--line2);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--ink);
  outline: none;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.field :deep(input:focus),
.field :deep(textarea:focus),
.field :deep(select:focus) {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.field :deep(input::placeholder),
.field :deep(textarea::placeholder) { color: var(--faint); }

.field-hint {
  display: block;
  font-size: 12px;
  color: var(--faint);
  margin-top: 6px;
  line-height: 1.5;
}
</style>
