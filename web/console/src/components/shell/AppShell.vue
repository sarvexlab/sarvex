<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useOperatorStore } from '@/stores/operator'
import { useAuthStore } from '@/stores/auth'
import { useStoresStore } from '@/stores/stores'
import { useJourneyStore } from '@/stores/journey'
import { useActivationStore } from '@/stores/activation'
import Sidebar from './Sidebar.vue'
import ThemeToggle from './ThemeToggle.vue'
import StoreSwitcher from './StoreSwitcher.vue'
import UserMenu from './UserMenu.vue'
import ReadinessPill from './ReadinessPill.vue'
import StoreStatusPill from './StoreStatusPill.vue'
import CommandPalette from './CommandPalette.vue'
import GoLiveCelebration from './GoLiveCelebration.vue'
import OperatorBar from '@/components/operator/OperatorBar.vue'
import AuditLog from '@/components/operator/AuditLog.vue'

const operator = useOperatorStore()
const auth = useAuthStore()
const stores = useStoresStore()
const journey = useJourneyStore()
const activation = useActivationStore()
const route = useRoute()
const { t } = useI18n()

// The live stream belongs to the shell, not to one surface — see the
// history in git blame if this looks like it should live in PathSurface
// again; it raced there (arriving/departing surfaces both touched it).
function syncStream() {
  if (stores.currentStoreId) journey.connectStream()
  else journey.disconnectStream()
}
onMounted(syncStream)
watch(() => stores.currentStoreId, syncStream)
onUnmounted(() => journey.disconnectStream())

// Activation backs the sidebar's Setup visibility and the topbar's status
// pill everywhere, not just on Home — load it here rather than depending on
// whichever surface happens to mount first.
function syncActivation() {
  if (stores.currentStoreId) activation.load()
}
onMounted(syncActivation)
watch(() => stores.currentStoreId, syncActivation)

const showSidebar = computed(() => {
  if (route.path === '/board') return false
  if (auth.isOperator && !operator.isImpersonating) return false
  return true
})

// Desktop collapse — persisted like the theme choice.
const COLLAPSE_KEY = 'sarvex.sidebarCollapsed'
const collapsed = ref(localStorage.getItem(COLLAPSE_KEY) === '1')
// Tablet widths: the rail is icons-only and can't expand (no room), whatever
// the saved desktop preference says.
const NARROW_QUERY = '(max-width: 1023px)'
const narrowMql = window.matchMedia?.(NARROW_QUERY)
const narrow = ref(narrowMql?.matches ?? false)
const onNarrowChange = (e: MediaQueryListEvent) => { narrow.value = e.matches }
onMounted(() => narrowMql?.addEventListener('change', onNarrowChange))
onUnmounted(() => narrowMql?.removeEventListener('change', onNarrowChange))

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem(COLLAPSE_KEY, collapsed.value ? '1' : '0')
}

// Mobile drawer.
const mobileNavOpen = ref(false)
watch(() => route.fullPath, () => { mobileNavOpen.value = false })
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') mobileNavOpen.value = false
}
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

// Only the content column scrolls (the shell is viewport-height), so a new
// page would otherwise open at the previous page's scroll offset.
const scrollEl = ref<HTMLElement | null>(null)
watch(() => route.path, () => scrollEl.value?.scrollTo({ top: 0 }))
</script>

<template>
  <div class="scene">
    <OperatorBar />

    <div class="layout">
      <Sidebar
        v-if="showSidebar"
        variant="desktop"
        class="desktop-sidebar"
        :collapsed="collapsed || narrow"
        :collapsible="!narrow"
        @toggle-collapsed="toggleCollapsed"
      />

      <div class="main-col">
        <header class="topbar">
          <div class="tb-left">
            <button
              v-if="showSidebar"
              type="button"
              class="icon-btn mobile-menu-btn"
              :aria-label="t('shell.sidebar.openMenu')"
              :aria-expanded="mobileNavOpen"
              @click="mobileNavOpen = true"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="4" y1="7" x2="20" y2="7" /><line x1="4" y1="12" x2="20" y2="12" /><line x1="4" y1="17" x2="20" y2="17" />
              </svg>
            </button>
            <StoreSwitcher v-if="showSidebar" />
          </div>

          <div v-if="showSidebar" class="tb-center">
            <StoreStatusPill />
          </div>
          <div v-else class="nav-spacer" />

          <div class="tb-right">
            <ReadinessPill />
            <CommandPalette v-if="showSidebar" />
            <ThemeToggle />
            <button class="icon-btn" :aria-label="t('shell.notifications')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
                <path d="M13.7 21a2 2 0 0 1-3.4 0" />
              </svg>
            </button>
            <UserMenu />
          </div>
        </header>

        <div ref="scrollEl" class="scroll">
          <slot />
        </div>
      </div>
    </div>

    <!-- Mobile drawer -->
    <Teleport to="body">
      <div v-if="mobileNavOpen" class="mobile-drawer-scrim" @click.self="mobileNavOpen = false">
        <Sidebar variant="mobile" @navigate="mobileNavOpen = false" @close="mobileNavOpen = false" />
      </div>
    </Teleport>

    <AuditLog v-if="operator.showAudit && operator.isImpersonating" />
    <GoLiveCelebration v-if="showSidebar" />
  </div>
</template>

<style scoped>
/* App-style shell: exactly the viewport, sidebar full height, only the
   content column scrolls. */
.scene { height: 100vh; height: 100dvh; display: flex; flex-direction: column; overflow: hidden; }
.layout { flex: 1; display: flex; min-height: 0; }
.desktop-sidebar { flex-shrink: 0; }
@media (max-width: 767px) {
  .desktop-sidebar { display: none; }
}

.main-col { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; }

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 0 26px;
  height: 58px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 78%, transparent);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
  position: sticky;
  top: 0;
  z-index: 40;
  /* Children respond to the topbar's own width, which the sidebar changes. */
  container: topbar / inline-size;
}

.tb-left { display: flex; align-items: center; gap: 14px; min-width: 0; flex: 1 1 0; }
.tb-left :deep(.store-switch-wrap),
.tb-left :deep(.store-switch) { min-width: 0; max-width: 100%; }
/* The status pill keeps its size; the store name truncates instead. */
.tb-center { flex: 0 0 auto; display: flex; justify-content: center; white-space: nowrap; }
.nav-spacer { flex: 1; }
.tb-right { display: flex; align-items: center; gap: 14px; flex-shrink: 0; }

@container topbar (max-width: 620px) {
  .tb-center { display: none; }
}
@media (max-width: 767px) {
  .topbar { padding: 0 14px; gap: 10px; }
  .tb-left { gap: 8px; }
  .tb-right { gap: 6px; }
}

.icon-btn {
  width: 33px;
  height: 33px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--text-muted);
  transition: background 0.15s, color 0.15s;
}
.icon-btn svg { width: 18px; height: 18px; }
.icon-btn:hover { background: var(--surface2); color: var(--text); }
.icon-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
/* After .icon-btn, whose display: grid would otherwise show it on desktop. */
.icon-btn.mobile-menu-btn { display: none; }
@media (max-width: 767px) {
  .icon-btn.mobile-menu-btn { display: grid; }
}

.scroll { flex: 1; min-height: 0; overflow-y: auto; }

.mobile-drawer-scrim {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 150;
  display: flex;
}
@media (prefers-reduced-motion: no-preference) {
  .mobile-drawer-scrim { animation: scrim-in 0.15s ease-out; }
}
@keyframes scrim-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>
