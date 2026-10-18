<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Toggle. Two-way bound via v-model so callers stay declarative.

const props = defineProps<{ modelValue: boolean; label?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

function toggle() {
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :class="['switch', { on: modelValue }]"
    @click="toggle"
  >
    <span class="knob" />
  </button>
</template>

<style scoped>
.switch {
  width: 42px;
  height: 24px;
  border-radius: 99px;
  background: var(--line2);
  position: relative;
  transition: background 0.18s;
  flex-shrink: 0;
  border: 0;
  padding: 0;
  cursor: pointer;
}
.switch.on { background: var(--accent); }
.knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.28);
  transition: transform 0.2s cubic-bezier(0.3, 1.3, 0.5, 1);
}
.switch.on .knob { transform: translateX(18px); }
.switch:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
</style>
