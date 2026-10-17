<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const { t } = useI18n()
const open = ref(false)

const initials = computed(() => {
  const email = auth.principal?.email ?? ''
  if (!email) return '—'
  const local = email.split('@')[0] ?? ''
  const parts = local.split(/[._-]/).filter(Boolean)
  const a = parts[0]?.[0] ?? local[0] ?? '?'
  const b = parts[1]?.[0] ?? local[1] ?? ''
  return (a + b).toUpperCase()
})

function close(e?: MouseEvent) {
  if (e && (e.target as HTMLElement).closest('.user-menu')) return
  open.value = false
}
onMounted(() => document.addEventListener('click', close))
onBeforeUnmount(() => document.removeEventListener('click', close))

async function onSignOut() {
  open.value = false
  await auth.signOut()
  router.replace('/login')
}
</script>

<template>
  <div class="user-menu">
    <button class="avatar" type="button" :aria-label="t('shell.userMenu.accountMenu')" @click="open = !open">
      {{ initials }}
    </button>
    <div v-if="open" class="menu" role="menu">
      <div class="who">
        <div class="name">{{ auth.principal?.email }}</div>
        <div class="role">{{ auth.principal?.role }}</div>
      </div>
      <button class="item" type="button" @click="onSignOut">{{ t('shell.userMenu.signOut') }}</button>
    </div>
  </div>
</template>

<style scoped>
.user-menu { position: relative; }
.avatar {
  width: 31px;
  height: 31px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--accent-deep);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  flex-shrink: 0;
}
.menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 49;
  width: 220px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg);
  padding: 7px;
}
.who { padding: 9px 11px 11px; border-bottom: 1px solid var(--line); margin-bottom: 5px; }
.name { font-weight: 600; font-size: 13.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.role { font-size: 11.5px; color: var(--faint); text-transform: uppercase; letter-spacing: 0.08em; margin-top: 3px; }
.item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 9px 11px;
  border-radius: var(--radius-sm);
  font-size: 13.5px;
  color: var(--ink);
}
.item:hover { background: var(--surface2); }
</style>
