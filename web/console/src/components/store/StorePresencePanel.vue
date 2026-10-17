<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Presence tab — plain-language summary + served-from-domain card + manifest tree.
// GET /presence, GET /presence/manifest → usePresenceStore

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStoresStore } from '@/stores/stores'
import { usePresenceStore } from '@/stores/presence'
import ManifestView from '@/components/store/ManifestView.vue'
import Chip from '@/components/ui/Chip.vue'
import Icon from '@/components/ui/Icon.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorBanner from '@/components/ui/ErrorBanner.vue'

const emit = defineEmits<{ retry: [] }>()

const stores = useStoresStore()
const presence = usePresenceStore()
const { t } = useI18n()

const store = computed(() => stores.currentStore)
// The profile is served as soon as the store is UCP active — it does not
// wait on any distribution partner, so neither does this panel.
const isLive = computed(() => store.value?.ucp_state === 'active')
const showSkel = computed(() => !presence.loaded && presence.loading)
const supportedVersions = computed(() => presence.presence?.supported_versions ?? [])
const publishedVersion = computed(() => presence.presence?.published_version)
</script>

<template>
  <ErrorBanner v-if="presence.error" :message="presence.error" @retry="emit('retry')" />
  <section v-if="showSkel" class="skel-block">
    <Skeleton height="68px" radius="11px" />
    <Skeleton height="86px" radius="11px" />
    <Skeleton height="220px" radius="11px" />
  </section>
  <section v-else-if="presence.presence && presence.manifest">
    <div v-if="!isLive" class="card summary-card draft">
      <span class="ic"><Icon name="clock" /></span>
      <div>
        <strong>{{ t('store.presence.notPublished.title') }}</strong>
        {{ t('store.presence.notPublished.body', { storeName: store?.name }) }}
      </div>
    </div>
    <div v-else class="card summary-card">
      <span class="ic"><Icon name="shield" /></span>
      <div><strong>{{ t('store.presence.plainLanguage') }}</strong> {{ presence.presence.summary }}</div>
    </div>

    <div class="card served-card">
      <div class="served-dot"><Icon name="store" /></div>
      <div class="served-body">
        <div class="eyebrow tight">{{ t('store.presence.servedFrom') }}</div>
        <div class="mono served-url">{{ presence.presence.profile_url }}</div>
      </div>
      <div class="served-meta">
        <Chip v-if="isLive" kind="ok"><Icon name="check" /> {{ t('store.presence.liveBadge') }}</Chip>
        <Chip v-else kind="quiet"><Icon name="clock" /> {{ t('store.presence.draftBadge') }}</Chip>
        <span class="meta-sub">{{ isLive ? t('store.presence.viaPlugin') : t('store.presence.publishesAtGoLive') }}</span>
      </div>
    </div>

    <div v-if="supportedVersions.length" class="versions-row">
      <span class="eyebrow tight">{{ t('store.presence.versionsLabel') }}</span>
      <Chip v-for="v in supportedVersions" :key="v" :kind="v === publishedVersion ? 'you' : 'us'">
        {{ v }}<template v-if="v === publishedVersion"> · {{ t('store.presence.publishedVersionSuffix') }}</template>
      </Chip>
    </div>

    <div class="eyebrow manifest-label">
      {{ isLive ? t('store.presence.yourProfile') : t('store.presence.draftProfile') }} · /.well-known/ucp
    </div>
    <div class="manifest mono">
      <span class="mf-punc">{</span>
      <ManifestView :obj="(presence.manifest.manifest as Record<string, unknown>) ?? {}" :indent="1" />
      <span class="mf-punc">}</span>
    </div>
    <p class="footnote">
      {{ t('store.presence.footnotePrefix') }}
      <span class="mono">{{ t('store.presence.footnoteEndpoint') }}</span>
      {{ t('store.presence.footnoteSuffix') }}
    </p>
  </section>
</template>

<style scoped>
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.summary-card {
  padding: 22px 24px;
  display: flex;
  gap: 16px;
  align-items: flex-start;
  background: var(--accent-softer);
  border-color: var(--accent-line);
  margin-bottom: 18px;
}
.summary-card .ic { color: var(--accent); flex-shrink: 0; margin-top: 2px; }
.summary-card .ic :deep(svg) { width: 22px; height: 22px; }
.summary-card.draft {
  background: var(--surface2);
  border-color: var(--line2);
}
.summary-card.draft .ic { color: var(--muted); }

.served-card {
  padding: 16px 20px;
  margin-bottom: 22px;
  display: flex;
  align-items: center;
  gap: 15px;
}
.served-dot {
  width: 34px;
  height: 34px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: var(--accent-soft);
  color: var(--accent-deep);
  flex-shrink: 0;
}
.served-dot :deep(svg) { width: 17px; height: 17px; }
.served-body { flex: 1; min-width: 0; }
.served-url { font-weight: 600; font-size: 14px; word-break: break-all; margin-top: 5px; }
.served-meta {
  display: flex;
  flex-direction: column;
  gap: 7px;
  align-items: flex-end;
  flex-shrink: 0;
  text-align: right;
}
.meta-sub { font-size: 11.5px; color: var(--faint); max-width: 15ch; line-height: 1.4; }

.eyebrow.tight { margin-bottom: 5px; }
.manifest-label { margin: 22px 0 10px; }

.versions-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 9px;
  margin-bottom: 22px;
}
.versions-row .eyebrow { margin-bottom: 0; margin-right: 2px; }

.manifest {
  font-size: 12.5px;
  line-height: 1.7;
  padding: 20px 22px;
  overflow-x: auto;
  background: var(--surface2);
  border-radius: var(--radius);
  border: 1px solid var(--line);
}
.mf-punc { color: var(--faint); }

.footnote {
  font-size: 12.5px;
  color: var(--faint);
  margin-top: 13px;
  line-height: 1.6;
  max-width: 68ch;
}

.skel-block { display: flex; flex-direction: column; gap: 16px; }
</style>
