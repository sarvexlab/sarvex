<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Where a merchant sees which agent surfaces their store reaches, and
// enrolls in more.
//
// The page is a loop over the registry. It has no knowledge of any
// particular surface: every card's title, description, state and whole
// enrollment flow comes from the descriptor on the record, rendered by one
// generic component. Adding a surface adds a card here for free.
//
// Activation is deliberately shown at the top and kept separate. A store
// that is UCP active with zero surfaces enrolled is finished, not
// half-finished — so the page says so plainly rather than presenting the
// surfaces below as remaining work.

import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSurfacesStore } from '@/stores/surfaces'
import { useActivationStore } from '@/stores/activation'
import { sortEnrollments, stateKind } from '@/lib/surface'
import SurfaceEnrollmentFlow from '@/components/surface/SurfaceEnrollmentFlow.vue'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const surfaces = useSurfacesStore()
const activation = useActivationStore()
const { t, te } = useI18n()

onMounted(() => {
  surfaces.load()
  activation.load()
})

function label(key: string | undefined, fallback = ''): string {
  if (!key) return fallback
  return te(key) ? t(key) : (key.split('.').pop() ?? fallback)
}

const cards = computed(() => sortEnrollments(surfaces.enrollments))
const showSkeleton = computed(() => !surfaces.loaded && surfaces.loading)
const enrolledCount = computed(() => surfaces.enrolled.length)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">{{ t('surfaces.eyebrow') }}</div>
      <h1 class="page-title display">{{ t('surfaces.title') }}</h1>
      <p class="page-sub">{{ t('surfaces.sub') }}</p>
    </div>

    <ErrorBanner v-if="surfaces.error" :message="surfaces.error" @retry="surfaces.load" />

    <!-- Activation, stated as its own fact. When the store is active this
         reads as a completion, not as a prerequisite for the cards below. -->
    <div class="card activation" :class="{ ok: activation.isActive }">
      <Icon :name="activation.isActive ? 'check' : 'clock'" :size="16" />
      <div class="activation-body">
        <div class="activation-title">
          {{ activation.isActive ? t('surfaces.activation.active') : t('surfaces.activation.pending') }}
        </div>
        <p class="activation-sub">
          {{
            activation.isActive
              ? t('surfaces.activation.activeSub', { count: enrolledCount })
              : t('surfaces.activation.pendingSub')
          }}
        </p>
      </div>
    </div>

    <template v-if="showSkeleton">
      <Skeleton v-for="i in 3" :key="i" height="180px" radius="11px" />
    </template>

    <div v-else class="cards">
      <section v-for="card in cards" :key="card.surface_id" class="card surface-card">
        <header class="surface-head">
          <div class="surface-id">
            <h2 class="surface-name">
              {{ label(card.descriptor?.display_name_key, card.surface_id) }}
            </h2>
            <Chip :kind="stateKind(card.state)">
              {{ t(`surfaces.state.${card.state}`) }}
            </Chip>
          </div>
          <p v-if="card.descriptor?.description_key" class="surface-desc">
            {{ label(card.descriptor.description_key) }}
          </p>
        </header>

        <!-- An auto-enrolling surface (direct_agent) has nothing to do and
             nothing to configure: it is a statement of fact about the
             store, so it renders as an informational card rather than
             offering actions that would mean nothing. -->
        <div v-if="card.descriptor?.auto_enroll" class="always-on">
          <Icon name="pulse" :size="14" />
          <span>
            {{
              card.state === 'enrolled'
                ? t('surfaces.alwaysOn.live')
                : t('surfaces.alwaysOn.pending')
            }}
          </span>
        </div>

        <SurfaceEnrollmentFlow v-else :enrollment="card" />
      </section>
    </div>
  </div>
</template>

<style scoped>
.page {
  max-width: 1080px;
  margin: 0 auto;
  padding: 40px 26px 130px;
}
.page-head {
  margin-bottom: 24px;
}
.page-title {
  font-size: 30px;
}
.page-sub {
  color: var(--muted);
  font-size: 16px;
  margin-top: 8px;
  max-width: 60ch;
}

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.activation {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 18px;
  margin-bottom: 24px;
  color: var(--muted);
}
.activation.ok {
  color: var(--ok);
}
.activation-body {
  flex: 1;
  min-width: 0;
}
.activation-title {
  font-weight: 600;
  color: var(--ink);
}
.activation-sub {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--muted);
}

.cards {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.surface-card {
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.surface-head {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.surface-id {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.surface-name {
  font-size: 17px;
  font-weight: 650;
  margin: 0;
}
.surface-desc {
  margin: 0;
  color: var(--muted);
  font-size: 13.5px;
  max-width: 65ch;
}

.always-on {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--muted);
  background: var(--surface2);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
}
</style>
