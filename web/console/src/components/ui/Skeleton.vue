<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Shimmery placeholder block. Width/height passed as CSS values so callers
// can size it to whatever they're standing in for (a row, a chip, a title).

defineProps<{ width?: string; height?: string; radius?: string }>()
</script>

<template>
  <span
    class="skel"
    :style="{ width: width ?? '100%', height: height ?? '14px', borderRadius: radius ?? '6px' }"
    aria-hidden="true"
  />
</template>

<style scoped>
.skel {
  display: inline-block;
  background: linear-gradient(
    90deg,
    var(--surface2) 0%,
    color-mix(in oklab, var(--surface2) 60%, var(--surface)) 50%,
    var(--surface2) 100%
  );
  background-size: 200% 100%;
  animation: skel 1.4s ease-in-out infinite;
}
@keyframes skel {
  0%   { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
@media (prefers-reduced-motion: reduce) {
  .skel { animation: none; }
}
</style>
