<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Primary navigation. Persistent full-height rail on desktop (collapsible to
// icons only, toggle in the footer), a slide-in drawer on mobile. Two call
// sites (AppShell) mount this once each, passing `variant` so the two respond
// to their own breakpoint rather than duplicating the nav markup.
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NAV, type NavItem } from '@/router'
import Icon from '@/components/ui/Icon.vue'
import Mark from './Mark.vue'

const props = withDefaults(defineProps<{
  variant: 'desktop' | 'mobile'
  collapsed?: boolean
  /** Desktop only: whether the rail may be toggled (off at tablet widths). */
  collapsible?: boolean
}>(), { collapsed: false, collapsible: true })
defineEmits<{ navigate: []; close: []; 'toggle-collapsed': [] }>()

const route = useRoute()
const { t } = useI18n()

// Only the desktop rail collapses; the mobile drawer always shows labels.
const iconsOnly = computed(() => props.variant === 'desktop' && !!props.collapsed)

function isActive(item: NavItem): boolean {
  const [path, query] = item.to.split('?')
  if (route.path !== path && route.path !== item.matchPath) return false
  if (!query) return route.path === path
  const wantTab = new URLSearchParams(query).get('tab')
  return (route.query.tab as string | undefined) === wantTab
}
</script>

<template>
  <nav :class="['sidebar', variant, { collapsed: iconsOnly }]" :aria-label="t('shell.sidebar.label')">
    <div class="sb-head">
      <div class="wordmark"><Mark /><span v-if="!iconsOnly">Sarvex</span></div>
      <button
        v-if="variant === 'mobile'"
        type="button"
        class="sb-icon-btn"
        :aria-label="t('shell.sidebar.closeMenu')"
        @click="$emit('close')"
      >
        <Icon name="x" :size="18" />
      </button>
    </div>

    <ul class="sb-list">
      <li v-for="item in NAV" :key="item.to">
        <router-link :to="item.to" custom v-slot="{ navigate }">
          <button
            type="button"
            class="sb-item"
            :class="{ active: isActive(item) }"
            :aria-current="isActive(item) ? 'page' : undefined"
            :aria-label="iconsOnly ? t(item.labelKey) : undefined"
            :title="iconsOnly ? t(item.labelKey) : undefined"
            @click="navigate(); $emit('navigate')"
          >
            <Icon :name="item.icon" :size="18" />
            <span v-if="!iconsOnly" class="sb-label">{{ t(item.labelKey) }}</span>
          </button>
        </router-link>
      </li>
    </ul>

    <div v-if="variant === 'desktop' && props.collapsible" class="sb-foot">
      <button
        type="button"
        class="sb-item sb-collapse"
        :aria-label="iconsOnly ? t('shell.sidebar.expand') : t('shell.sidebar.collapse')"
        :title="iconsOnly ? t('shell.sidebar.expand') : t('shell.sidebar.collapse')"
        :aria-expanded="!iconsOnly"
        @click="$emit('toggle-collapsed')"
      >
        <Icon name="panel" :size="18" />
      </button>
    </div>
  </nav>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 14px 10px;
  background: var(--surface);
  border-right: 1px solid var(--border);
  /* Fill the column the shell gives it; the list scrolls if it ever outgrows it. */
  align-self: stretch;
  min-height: 0;
  overflow: hidden;
}
.sidebar.desktop { width: 232px; transition: width 0.18s ease; }
.sidebar.desktop.collapsed { width: 60px; }

.sb-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  height: 36px;
  padding: 0 8px;
  flex-shrink: 0;
}
.collapsed .sb-head { justify-content: center; padding: 0; }
.wordmark {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-display);
  font-size: 18px;
  letter-spacing: -0.02em;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
}

.sb-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.sb-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  font-size: 14px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  transition: background-color 0.15s, color 0.15s;
}
.collapsed .sb-item { justify-content: center; padding: 0; }
.sb-item :deep(svg) { flex-shrink: 0; }
.sb-label { overflow: hidden; text-overflow: ellipsis; }
.sb-item:hover { color: var(--text); background: var(--surface2); }
.sb-item:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.sb-item.active { color: var(--text); background: var(--surface2); }
.sb-item.active :deep(svg) { color: var(--accent); }
/* Active marker on the rail's inner edge. */
.sb-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: var(--accent);
}

.sb-foot {
  flex-shrink: 0;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
.sb-collapse { width: 36px; padding: 0; justify-content: center; color: var(--text-faint); }
.collapsed .sb-collapse { width: 100%; }

.sb-icon-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--text-muted);
}
.sb-icon-btn:hover { background: var(--surface2); color: var(--text); }
.sb-icon-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

.sidebar.mobile {
  width: 280px;
  max-width: 84vw;
  height: 100%;
  box-shadow: var(--shadow-lg);
}
@media (prefers-reduced-motion: no-preference) {
  .sidebar.mobile { animation: drawer-in 0.2s cubic-bezier(0.3, 1, 0.4, 1); }
}
@keyframes drawer-in {
  from { transform: translateX(-100%); }
  to { transform: translateX(0); }
}
</style>
