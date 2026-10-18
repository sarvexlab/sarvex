<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Recursive JSON tree for the UCP manifest. Renders the served document
// indented with key/value coloring per the design. Pure presentational —
// the data shape is opaque (the UCP profile schema is external to Sarvex's
// own API).

import { computed } from 'vue'

const props = defineProps<{ obj: Record<string, unknown>; indent?: number }>()

const entries = computed(() => Object.entries(props.obj))
const padPx = computed(() => (props.indent ?? 0) * 16)

function isObj(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object' && !Array.isArray(v)
}
function isArr(v: unknown): v is unknown[] {
  return Array.isArray(v)
}
function isPrimArr(arr: unknown[]): boolean {
  return arr.every((x) => x === null || typeof x !== 'object')
}

function fmtScalar(v: unknown): string {
  if (typeof v === 'string') return `"${v}"`
  if (v === null) return 'null'
  return String(v)
}
function scalarClass(v: unknown): string {
  if (typeof v === 'string') return 'mf-str'
  if (typeof v === 'number') return 'mf-num'
  if (typeof v === 'boolean') return 'mf-bool'
  if (v === null) return 'mf-punc'
  return ''
}
</script>

<template>
  <template v-for="([k, v], i) in entries" :key="k">
    <div :style="{ paddingLeft: padPx + 'px' }">
      <span class="mf-key">"{{ k }}"</span><span class="mf-punc">: </span>

      <template v-if="isObj(v)">
        <span class="mf-punc">{</span>
        <ManifestView :obj="v" :indent="(indent ?? 0) + 1" />
        <span :style="{ paddingLeft: padPx + 'px' }" class="mf-punc">
          }{{ i < entries.length - 1 ? ',' : '' }}
        </span>
      </template>

      <template v-else-if="isArr(v)">
        <template v-if="v.length === 0">
          <span class="mf-punc">[]{{ i < entries.length - 1 ? ',' : '' }}</span>
        </template>
        <template v-else-if="isPrimArr(v)">
          <span class="mf-punc">[</span>
          <template v-for="(x, j) in v" :key="j">
            <span :class="scalarClass(x)">{{ fmtScalar(x) }}</span>
            <span v-if="j < v.length - 1" class="mf-punc">, </span>
          </template>
          <span class="mf-punc">]{{ i < entries.length - 1 ? ',' : '' }}</span>
        </template>
        <template v-else>
          <span class="mf-punc">[</span>
          <div v-for="(x, j) in v" :key="j" :style="{ paddingLeft: padPx + 16 + 'px' }">
            <span class="mf-punc">{</span>
            <ManifestView :obj="(x as Record<string, unknown>)" :indent="(indent ?? 0) + 2" />
            <span :style="{ paddingLeft: padPx + 16 + 'px' }" class="mf-punc">
              }{{ j < v.length - 1 ? ',' : '' }}
            </span>
          </div>
          <span :style="{ paddingLeft: padPx + 'px' }" class="mf-punc">
            ]{{ i < entries.length - 1 ? ',' : '' }}
          </span>
        </template>
      </template>

      <template v-else>
        <span :class="scalarClass(v)">{{ fmtScalar(v) }}</span><span
          class="mf-punc"
        >{{ i < entries.length - 1 ? ',' : '' }}</span>
      </template>
    </div>
  </template>
</template>

<style scoped>
.mf-key { color: var(--accent-deep); }
.mf-str { color: var(--ok); }
.mf-num { color: var(--warn); }
.mf-bool { color: var(--accent); }
.mf-punc { color: var(--faint); }
</style>
