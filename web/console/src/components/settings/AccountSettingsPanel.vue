<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Account band: shared across all of the merchant's stores. Every row gets
// the design's Edit / Manage / Change / Update / View action that opens the
// single SettingsDrawer to the matching panel (via the open-panel emit).
//
// Endpoints:
//   /account, /account/members  → useAccountStore
//   /billing, billing/portal-link → useBillingStore
//   /notifications/preferences  → useNotificationsStore

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAccountStore } from '@/stores/account'
import { useBillingStore } from '@/stores/billing'
import { useNotificationsStore } from '@/stores/notifications'
import Chip from '@/components/ui/Chip.vue'
import Switch from '@/components/ui/Switch.vue'
import Icon from '@/components/ui/Icon.vue'
import type { SettingsPanel } from '@/components/settings/SettingsDrawer.vue'
import { planName } from '@/lib/plans'

const emit = defineEmits<{ (e: 'open-panel', panel: SettingsPanel): void }>()

const account = useAccountStore()
const billing = useBillingStore()
const notifications = useNotificationsStore()
const { t } = useI18n()

const ownerMember = computed(() => account.members.find((m) => m.role === 'owner') ?? null)

const planLabel = computed(() => planName(t, billing.billing?.plan))
const billingStatus = computed(() => billing.billing?.status ?? '—')

const taskEmails = computed({
  get: () => notifications.prefs?.email_enabled ?? false,
  set: async (v) => {
    if (!notifications.prefs) return
    await notifications.save({ ...notifications.prefs, email_enabled: v })
  },
})
const weeklyDigest = computed({
  get: () => notifications.prefs?.weekly_digest ?? false,
  set: async (v) => {
    if (!notifications.prefs) return
    await notifications.save({ ...notifications.prefs, weekly_digest: v })
  },
})

function openPanel(p: SettingsPanel) {
  emit('open-panel', p)
}
</script>

<template>
  <section class="band">
    <div class="band-head">
      <span class="band-ic"><Icon name="user" /></span>
      <div>
        <div class="band-title">{{ t('settings.account.title') }}</div>
        <div class="band-sub">
          {{ t('settings.account.sharedAcross') }} · {{ account.account?.name ?? '—' }}
        </div>
      </div>
    </div>

    <div class="section-label">{{ t('settings.account.sectionBusinessTeam') }}<span class="ln" /></div>
    <div class="card list">
      <div class="row first">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.businessName') }}</div>
          <div class="row-value">
            {{ account.account?.name ?? '—' }}
            <template v-if="account.account?.business_facts?.legal_entity_name">
              · {{ account.account.business_facts.legal_entity_name }}
            </template>
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('name')">{{ t('settings.account.edit') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.locale') }}</div>
          <div class="row-value">
            <!-- eslint-disable-next-line @typescript-eslint/no-explicit-any -->
            {{ account.account?.timezone || '—' }}{{ (account.account as any)?.country_code ? ` · ${(account.account as any).country_code}` : '' }}
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('locale')">{{ t('settings.account.edit') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.accountOwner') }}</div>
          <div class="row-value">{{ ownerMember?.email ?? '—' }}</div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('owner')">{{ t('settings.account.edit') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.team') }}</div>
          <div class="row-value">
            {{ account.members.length }} {{ account.members.length === 1 ? t('settings.account.personSingular') : t('settings.account.personPlural') }} ·
            {{ t('settings.account.signInVia') }}
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('team')">{{ t('settings.account.manage') }}</button>
      </div>
    </div>

    <div class="section-label">{{ t('settings.account.sectionNotifications') }}<span class="ln" /></div>
    <div class="card list">
      <div class="row first">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.taskEmails') }}</div>
          <div class="row-value">
            {{ t('settings.account.taskEmailsDesc') }}
            {{ taskEmails ? t('settings.account.on') : t('settings.account.off') }}
          </div>
        </div>
        <Switch v-model="taskEmails" :label="t('settings.account.taskEmails')" />
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.weeklyDigest') }}</div>
          <div class="row-value">
            {{ t('settings.account.weeklyDigestDesc') }} {{ weeklyDigest ? t('settings.account.on') : t('settings.account.off') }}
          </div>
        </div>
        <Switch v-model="weeklyDigest" :label="t('settings.account.weeklyDigest')" />
      </div>
    </div>

    <div class="section-label">{{ t('settings.account.sectionBilling') }}<span class="ln" /></div>
    <div class="card list">
      <div class="row first">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.plan') }}</div>
          <div class="row-value">
            {{ planLabel }} ·
            <Chip :kind="billingStatus === 'active' ? 'ok' : 'warn'">{{ billingStatus }}</Chip>
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('plan')">{{ t('settings.account.change') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.payment') }}</div>
          <div class="row-value">{{ t('settings.account.paymentDesc') }}</div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('payment')">{{ t('settings.account.update') }}</button>
      </div>
      <div class="divider" />
      <div class="row">
        <div class="row-main">
          <div class="row-label">{{ t('settings.account.invoices') }}</div>
          <div class="row-value">
            <span class="mono">{{ billing.billing?.usage?.sku_count ?? '—' }}</span>
            / <span class="mono">{{ billing.billing?.usage?.sku_cap ?? '—' }}</span>
            {{ t('settings.account.skus') }}
            <template v-if="billing.billing?.sku_band">
              · {{ t('settings.account.band') }} <span class="mono">{{ billing.billing.sku_band }}</span>
            </template>
          </div>
        </div>
        <button class="btn-ghost" type="button" @click="openPanel('invoices')">{{ t('settings.account.view') }}</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.band { margin-bottom: 30px; }
.band-head { display: flex; align-items: center; gap: 13px; margin: 6px 0 4px; }
.band-ic {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: var(--accent-soft);
  color: var(--accent-deep);
}
.band-ic :deep(svg) { width: 19px; height: 19px; }
.band-title {
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 21px;
  letter-spacing: var(--display-tracking);
}
.band-sub { font-size: 13px; color: var(--muted); margin-top: 1px; }

.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 24px 4px 10px;
}
.section-label .ln { flex: 1; height: 1px; background: var(--line); }

.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.list { overflow: hidden; }
.row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 22px;
  border-top: 1px solid var(--line);
}
.row.first { border-top: 0; }
.row-main { flex: 1; min-width: 0; }
.row-label { font-weight: 500; }
.row-value {
  color: var(--muted);
  font-size: 13.5px;
  margin-top: 3px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.row-value.mono { font-family: var(--font-mono); }
.divider { height: 1px; background: var(--line); border: 0; }

.btn-ghost {
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
  border: 0;
  border-radius: var(--radius-sm);
  padding: 7px 13px;
  font-weight: 600;
  font-size: 12.5px;
  cursor: pointer;
  white-space: nowrap;
}
.btn-ghost:hover { box-shadow: inset 0 0 0 1px var(--faint); }
</style>
