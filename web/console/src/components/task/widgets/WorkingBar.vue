<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// The "Sarvex is working on this" indicator for widgets whose underlying job
// is genuinely in flight.
//
// The design reference draws a determinate bar (55%, 42%) for catalog import
// and audit, but no control-plane endpoint exposes a completion ratio for
// either job — `readiness.last_audit_at` and `connector.last_full_sync_at`
// tell us *whether* the job has finished, not how far along it is. Rather
// than invent a percentage, this renders the same bar shape running
// indeterminately, keeping the design's rhythm without asserting a number
// we don't have.

import { useI18n } from 'vue-i18n'

defineProps<{ label?: string }>()

const { t } = useI18n()
</script>

<template>
  <div class="working">
    <div class="pbar us" role="progressbar" :aria-label="t('task.widgets.workingBar.ariaLabel')">
      <i />
    </div>
    <span v-if="label" class="working-label">{{ label }}</span>
  </div>
</template>

<style scoped>
.working {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 5px;
}
.pbar {
  height: 6px;
  border-radius: 99px;
  background: var(--surface2);
  overflow: hidden;
  width: 120px;
  position: relative;
}
.pbar > i {
  display: block;
  height: 100%;
  width: 40%;
  border-radius: 99px;
  background: var(--muted);
}
@media (prefers-reduced-motion: no-preference) {
  .pbar > i { animation: slide 1.6s ease-in-out infinite; }
  @keyframes slide {
    0%   { transform: translateX(-100%); }
    100% { transform: translateX(250%); }
  }
}
.working-label {
  font-size: 12px;
  color: var(--muted);
}
</style>
