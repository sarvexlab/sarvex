<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// ⌘K / Ctrl+K palette (brief §Phase 2). v1 scope: navigate the primary nav
// and trigger "Run all checks" — product/order search is a natural
// extension once Catalog/Orders have their own filtered list views
// (Phase 5), not stubbed here with fake results.
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NAV } from '@/router'
import { useActivationStore } from '@/stores/activation'
import Icon from '@/components/ui/Icon.vue'

const router = useRouter()
const { t } = useI18n()
const activation = useActivationStore()

const open = ref(false)
const query = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

type Entry = { id: string; label: string; hint?: string; run: () => void }

const entries = computed<Entry[]>(() => [
  ...NAV.map((item) => ({
    id: `nav:${item.to}`,
    label: t(item.labelKey),
    hint: t('shell.commandPalette.goTo'),
    run: () => router.push(item.to),
  })),
  {
    id: 'action:run-checks',
    label: t('shell.commandPalette.runAllChecks'),
    hint: t('shell.commandPalette.action'),
    run: () => activation.verify(),
  },
])

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return entries.value
  return entries.value.filter((e) => e.label.toLowerCase().includes(q))
})

function show() {
  open.value = true
  query.value = ''
  nextTick(() => inputEl.value?.focus())
}
function hide() {
  open.value = false
}
function choose(entry: Entry) {
  entry.run()
  hide()
}

function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    open.value ? hide() : show()
  } else if (e.key === 'Escape' && open.value) {
    hide()
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

defineExpose({ show })
</script>

<template>
  <button type="button" class="cmdk-trigger" :aria-label="t('shell.commandPalette.open')" @click="show">
    <Icon name="zap" :size="16" />
    <span class="cmdk-kbd mono">⌘K</span>
  </button>

  <Teleport to="body">
    <div v-if="open" class="cmdk-overlay" @click.self="hide">
      <div class="cmdk-panel card" role="dialog" aria-modal="true" :aria-label="t('shell.commandPalette.title')">
        <div class="cmdk-search">
          <Icon name="zap" :size="16" />
          <input
            ref="inputEl"
            v-model="query"
            type="text"
            :placeholder="t('shell.commandPalette.placeholder')"
            :aria-label="t('shell.commandPalette.search')"
          />
        </div>
        <ul class="cmdk-list">
          <li v-for="entry in filtered" :key="entry.id">
            <button type="button" class="cmdk-item" @click="choose(entry)">
              <span>{{ entry.label }}</span>
              <span v-if="entry.hint" class="cmdk-hint">{{ entry.hint }}</span>
            </button>
          </li>
          <li v-if="!filtered.length" class="cmdk-empty">{{ t('shell.commandPalette.noMatches') }}</li>
        </ul>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cmdk-trigger {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 33px;
  padding: 0 10px;
  border-radius: 999px;
  color: var(--text-muted);
  border: 1px solid var(--border);
}
.cmdk-trigger:hover { background: var(--surface2); color: var(--text); }
.cmdk-trigger:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.cmdk-kbd { font-size: 11px; }
@container topbar (max-width: 520px) {
  .cmdk-trigger { width: 33px; padding: 0; justify-content: center; }
  .cmdk-kbd { display: none; }
}

.cmdk-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 14vh;
  z-index: 200;
}
.cmdk-panel {
  width: 100%;
  max-width: 520px;
  margin: 0 16px;
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.cmdk-search {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
  color: var(--text-faint);
}
.cmdk-search input {
  flex: 1;
  background: none;
  border: 0;
  color: var(--text);
  font-size: 15px;
}
.cmdk-search input:focus { outline: none; }

.cmdk-list { list-style: none; margin: 0; padding: 6px; max-height: 320px; overflow-y: auto; }
.cmdk-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 10px 10px;
  border-radius: var(--radius-sm);
  color: var(--text);
  font-size: 14px;
}
.cmdk-item:hover { background: var(--surface2); }
.cmdk-item:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.cmdk-hint { color: var(--text-faint); font-size: 12px; }
.cmdk-empty { padding: 16px; text-align: center; color: var(--text-faint); font-size: 14px; }
</style>
