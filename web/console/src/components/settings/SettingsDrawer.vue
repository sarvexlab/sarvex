<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// One drawer, switched by `panel`. Same right-side slide-in chrome as the
// other drawers; the body + footer change per panel.
//
// The OpenAPI is the contract:
//   - name:    PATCH /account (account.name)
//   - owner:   read-only — owner identity is derived from members[role=owner]
//              and CHANGED via the members endpoints (operator-assisted in V1)
//   - team:    GET /account/members (read-only list with role + status)
//   - plan:    select → POST /billing/checkout-link (Stripe-hosted completes)
//   - payment: POST /billing/portal-link
//   - invoices: POST /billing/portal-link (no GET endpoint exists)
//   - domain:  read-only view; domain isn't editable via the current contract
//   - return:  PUT /stores/{id}/profile (return_policy_url — per store, not per account)
//   - connector: GET /connectors/{id} + POST /connectors/{id}/resync
//   - handlers: PUT /stores/{id}/payment-config (per-handler enabled flag)
//   - profile: GET/PUT /stores/{id}/profile (store identity + fulfillment)

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { isAxiosError } from 'axios'
import type { components } from '@/api/schema'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useBillingStore } from '@/stores/billing'
import { useConnectorsStore } from '@/stores/connectors'
import { usePaymentStore } from '@/stores/payment'
import { useStoreProfileStore } from '@/stores/storeProfile'
import { useStoresStore } from '@/stores/stores'
import Chip from '@/components/ui/Chip.vue'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'
import Switch from '@/components/ui/Switch.vue'
import { resyncConnector, disconnectConnector } from '@/api/connectors'
import { formatFriendlyDateTime } from '@/lib/datetime'
import { buildPlans } from '@/lib/plans'

export type SettingsPanel =
  | 'name'
  | 'owner'
  | 'team'
  | 'locale'
  | 'plan'
  | 'payment'
  | 'invoices'
  | 'domain'
  | 'return'
  | 'connector'
  | 'handlers'
  | 'profile'

type ShippingZone = components['schemas']['shipping_zone']

const props = defineProps<{ panel: SettingsPanel }>()
const emit = defineEmits<{ close: []; toast: [message: string] }>()

const router = useRouter()
const { t } = useI18n()
const stores = useStoresStore()
const account = useAccountStore()
const auth = useAuthStore()
const billing = useBillingStore()
const connectors = useConnectorsStore()
const payment = usePaymentStore()
const storeProfile = useStoreProfileStore()

const store = computed(() => stores.currentStore)
const connector = computed(() =>
  store.value?.store_id ? connectors.forStore(store.value.store_id) : null,
)
const ownerMember = computed(() => account.members.find((m) => m.role === 'owner') ?? null)

// ── Team seats ──
// The seat cap is billing-plan data (GET /billing → usage.member_cap), not a
// client-side constant, so a plan change is reflected here without a redeploy.
const memberCap = computed(() => billing.billing?.usage?.member_cap ?? 5)
const atMemberCap = computed(() => account.members.length >= memberCap.value)

// ── Per-panel form state (seeded once on mount; drawer remounts via key) ──
const name = ref(account.account?.name ?? '')
const timezone = ref(account.account?.timezone ?? '')
const countryCode = ref(account.account?.country_code ?? '')

// ── Locale dropdowns (country → timezone) ──
// The server is the source of truth for which countries/timezones are
// selectable and which timezones belong to which country — the client
// never validates pairs itself, just renders what GET /account returned.
const localeCountries = computed(() => account.account?.locale_options?.countries ?? [])
const timeZoneOptions = computed(
  () => localeCountries.value.find((c) => c.code === countryCode.value)?.time_zones ?? [],
)
watch(countryCode, () => {
  if (!timeZoneOptions.value.includes(timezone.value)) {
    timezone.value = ''
  }
})
const returnPolicyUrl = ref(storeProfile.profile?.return_policy_url ?? '')
const handlerEnabled = ref<Record<string, boolean>>(
  Object.fromEntries((payment.config?.handlers ?? []).map((h) => [h.handler_id ?? '', !!h.enabled])),
)

// ── Store-profile form (legal identity + fulfillment) ──
const legalName = ref(storeProfile.profile?.legal_name ?? '')
const supportEmail = ref(storeProfile.profile?.support_email ?? '')
const profileDomain = ref(storeProfile.profile?.domain ?? store.value?.domain ?? '')
const fulfillmentRegions = ref<string[]>(storeProfile.profile?.fulfillment_regions ?? [])

// ── Team invite form ──
const inviteEmail = ref('')
const inviteError = ref<string | null>(null)
const inviting = ref(false)

const submitting = ref(false)
const confirmDisconnect = ref(false)

const meta = computed(() => ({
  title: t(`settings.drawer.meta.${props.panel}.title`),
  sub: t(`settings.drawer.meta.${props.panel}.sub`),
}))

const PLANS = computed(() => buildPlans(t))

// ── Actions ──────────────────────────────────────────────────────────────
async function onSaveName() {
  if (submitting.value || !name.value.trim()) return
  submitting.value = true
  try {
    await account.save({ name: name.value.trim() })
    emit('toast', t('settings.drawer.toasts.nameUpdated'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

async function onSaveLocale() {
  if (submitting.value) return
  submitting.value = true
  try {
    await account.save({
      timezone: timezone.value || undefined,
      country_code: countryCode.value || undefined,
    })
    emit('toast', t('settings.drawer.toasts.localeUpdated'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

async function onSaveReturn() {
  if (!store.value?.store_id || submitting.value) return
  submitting.value = true
  try {
    // Full-replacement PUT: carry the rest of the profile forward unchanged.
    await storeProfile.save(store.value.store_id, {
      legal_name: storeProfile.profile?.legal_name ?? '',
      support_email: storeProfile.profile?.support_email ?? '',
      domain: storeProfile.profile?.domain ?? '',
      fulfillment_regions: storeProfile.profile?.fulfillment_regions ?? [],
      shipping_zones: (storeProfile.profile?.shipping_zones ?? {}) as Record<string, ShippingZone>,
      return_policy_url: returnPolicyUrl.value.trim(),
    })
    emit('toast', t('settings.drawer.toasts.returnUpdated'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

// Unlike the other panel actions, a successful invite doesn't close the
// drawer — the owner is very likely inviting more than one teammate in a row.
async function onInviteMember() {
  const email = inviteEmail.value.trim()
  if (!email || inviting.value || atMemberCap.value) return
  inviting.value = true
  inviteError.value = null
  try {
    await account.invite(email)
    inviteEmail.value = ''
    emit('toast', t('settings.drawer.toasts.invited', { email }))
  } catch (err) {
    if (isAxiosError(err) && err.response?.data?.error?.code === 'member_already_active') {
      inviteError.value = t('settings.drawer.errors.memberAlreadyActive')
    } else if (isAxiosError(err) && err.response?.data?.error?.code === 'member_limit_reached') {
      inviteError.value = t('settings.drawer.errors.memberLimitReached', { cap: memberCap.value })
    } else {
      inviteError.value = err instanceof Error ? err.message : t('settings.drawer.errors.inviteFailedDefault')
    }
  } finally {
    inviting.value = false
  }
}

async function onOpenPortal() {
  await billing.openPortal()
  emit('toast', t('settings.drawer.toasts.openingPortal'))
  emit('close')
}

async function onResync() {
  if (!connector.value?.connector_id || submitting.value) return
  submitting.value = true
  try {
    await resyncConnector(connector.value.connector_id)
    emit('toast', t('settings.drawer.toasts.resyncQueued'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

async function onDisconnect() {
  if (!connector.value?.connector_id || submitting.value) return
  submitting.value = true
  try {
    await disconnectConnector(connector.value.connector_id)
    emit('toast', t('settings.drawer.toasts.disconnected'))
    emit('close')
    // Refresh before navigating: needsConnect (router/index.ts) reads the
    // connectors store directly, not the journey stream, so without this
    // the guard still sees the now-deleted connector as present and the
    // merchant stays parked on whatever page they were on until a manual
    // reload re-fetches everything from scratch.
    await connectors.load()
    await router.push({ name: 'connect' })
  } catch (err) {
    emit('toast', t('settings.drawer.toasts.disconnectFailed', { error: err instanceof Error ? err.message : t('settings.drawer.errors.disconnectUnknown') }))
  } finally {
    submitting.value = false
    confirmDisconnect.value = false
  }
}

async function onSaveProfile() {
  if (!store.value?.store_id || submitting.value) return
  submitting.value = true
  try {
    await storeProfile.save(store.value.store_id, {
      legal_name: legalName.value.trim(),
      support_email: supportEmail.value.trim(),
      domain: profileDomain.value.trim(),
      fulfillment_regions: fulfillmentRegions.value,
      // Full-replacement PUT: preserve fields not edited on this panel.
      return_policy_url: storeProfile.profile?.return_policy_url ?? '',
      shipping_zones: (storeProfile.profile?.shipping_zones ?? {}) as Record<string, ShippingZone>,
    })
    // The "Domain & profile" panel reads store.domain (denormalized on the
    // store list, not the profile) — patch it in place so it doesn't show
    // stale data until the next full stores.load().
    if (storeProfile.profile?.domain) {
      stores.setDomain(store.value.store_id, storeProfile.profile.domain)
    }
    emit('toast', t('settings.drawer.toasts.profileUpdated'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

async function onSaveHandlers() {
  if (!store.value?.store_id || submitting.value) return
  submitting.value = true
  try {
    await payment.save(store.value.store_id, {
      handlers: Object.entries(handlerEnabled.value).map(([handler_id, enabled]) => ({
        handler_id,
        enabled,
      })),
    })
    emit('toast', t('settings.drawer.toasts.handlersUpdated'))
    emit('close')
  } finally {
    submitting.value = false
  }
}

// Re-seed handler toggles if the config arrives after mount.
watch(
  () => payment.config?.handlers,
  (hs) => {
    if (!hs) return
    handlerEnabled.value = Object.fromEntries(hs.map((h) => [h.handler_id ?? '', !!h.enabled]))
  },
)

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="scrim" @click="emit('close')" />
  <aside class="drawer" role="dialog" aria-modal="true">
    <div class="drawer-head">
      <div class="head-top">
        <div>
          <h2 class="display title">{{ meta.title }}</h2>
          <p class="sub">{{ meta.sub }}</p>
        </div>
        <button class="icon-btn" :aria-label="t('common.close')" @click="emit('close')">
          <Icon name="x" />
        </button>
      </div>
    </div>

    <div class="drawer-body">
      <!-- ── name ── -->
      <template v-if="panel === 'name'">
        <Field :label="t('settings.drawer.fields.businessName')">
          <input v-model="name" type="text" :placeholder="t('settings.drawer.fields.businessNamePlaceholder')" @keydown.enter="onSaveName" />
        </Field>
      </template>

      <!-- ── locale ── -->
      <template v-else-if="panel === 'locale'">
        <Field :label="t('settings.drawer.fields.country')" :hint="t('settings.drawer.fields.countryHint')">
          <select v-model="countryCode">
            <option value="">{{ t('settings.drawer.fields.selectCountry') }}</option>
            <option v-for="c in localeCountries" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
        </Field>
        <Field :label="t('settings.drawer.fields.timezone')" :hint="t('settings.drawer.fields.timezoneHint')">
          <select v-model="timezone" :disabled="!countryCode">
            <option value="">{{ countryCode ? t('settings.drawer.fields.selectTimezone') : t('settings.drawer.fields.selectCountryFirst') }}</option>
            <option v-for="tz in timeZoneOptions" :key="tz" :value="tz">{{ tz }}</option>
          </select>
        </Field>
      </template>

      <!-- ── owner ── -->
      <template v-else-if="panel === 'owner'">
        <div class="kv-card">
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.currentOwner') }}</div>
            <div class="v">{{ ownerMember?.email ?? '—' }}</div>
          </div>
        </div>
        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.ownerNote') }}
          </span>
        </div>
      </template>

      <!-- ── team ── -->
      <template v-else-if="panel === 'team'">
        <div class="kv-card">
          <div
            v-for="(m, i) in account.members"
            :key="m.user_id ?? m.email"
            class="team-row"
            :class="{ first: i === 0 }"
          >
            <div class="avatar">{{ (m.email ?? '?').slice(0, 2).toUpperCase() }}</div>
            <div class="team-main">
              <div class="team-email">{{ m.email }}</div>
            </div>
            <div class="team-chips">
              <Chip :kind="m.role === 'owner' ? 'you' : 'quiet'">{{ m.role }}</Chip>
              <Chip v-if="m.status === 'invited'" kind="quiet">{{ t('settings.drawer.fields.invited') }}</Chip>
            </div>
          </div>
        </div>

        <template v-if="auth.isOwner">
          <Field
            :label="t('settings.drawer.fields.inviteLabel')"
            :hint="t('settings.drawer.fields.inviteHint', { used: account.members.length, cap: memberCap })"
          >
            <div class="invite-row">
              <input
                v-model="inviteEmail"
                type="email"
                :placeholder="t('settings.drawer.fields.invitePlaceholder')"
                :disabled="atMemberCap"
                @keydown.enter="onInviteMember"
              />
              <button
                class="btn btn-primary"
                type="button"
                :disabled="!inviteEmail.trim() || inviting || atMemberCap"
                @click="onInviteMember"
              >
                {{ inviting ? t('settings.drawer.fields.inviting') : t('settings.drawer.fields.invite') }}
              </button>
            </div>
          </Field>
          <div v-if="inviteError" class="note note-error">
            <Icon name="info" />
            <span>{{ inviteError }}</span>
          </div>
          <div v-else-if="atMemberCap" class="note">
            <Icon name="info" />
            <span>
              {{ t('settings.drawer.fields.seatsUsedNote', { cap: memberCap }) }}
            </span>
          </div>
        </template>

        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.signInNoteBase') }}
            <template v-if="!auth.isOwner">{{ t('settings.drawer.fields.signInNoteNonOwner') }}</template>
            <template v-else>{{ t('settings.drawer.fields.signInNoteOwner') }}</template>
          </span>
        </div>
      </template>

      <!-- ── plan ── -->
      <template v-else-if="panel === 'plan'">
        <div
          v-for="p in PLANS"
          :key="p.id"
          :class="['plan-opt', { sel: billing.billing?.plan === p.id }]"
        >
          <span class="plan-main">
            <span class="plan-top">
              <span class="plan-name">{{ p.name }}</span>
              <span class="plan-price-wrap">
                <span class="plan-price mono">{{ p.price }}</span>
                <span class="plan-cadence">{{ p.cadence }}</span>
              </span>
            </span>
            <span class="plan-blurb">{{ p.blurb }}</span>
          </span>
          <Chip v-if="billing.billing?.plan === p.id" kind="you">{{ t('settings.drawer.fields.current') }}</Chip>
        </div>
        <div class="note">
          <Icon name="shield" />
          <span>
            {{ t('settings.drawer.fields.planNote') }}
          </span>
        </div>
      </template>

      <!-- ── payment ── -->
      <template v-else-if="panel === 'payment'">
        <div class="kv-card">
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.cardOnFile') }}</div>
            <div class="v">{{ t('settings.drawer.fields.cardManaged') }}</div>
          </div>
        </div>
        <div class="note">
          <Icon name="shield" />
          <span>
            {{ t('settings.drawer.fields.paymentNote') }}
          </span>
        </div>
      </template>

      <!-- ── invoices ── -->
      <template v-else-if="panel === 'invoices'">
        <div class="kv-card">
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.invoicesLabel') }}</div>
            <div class="v">{{ t('settings.drawer.fields.invoicesValue') }}</div>
          </div>
        </div>
        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.invoicesNote') }}
          </span>
        </div>
      </template>

      <!-- ── domain ── -->
      <template v-else-if="panel === 'domain' && store">
        <Field :label="t('settings.drawer.fields.storefrontDomain')">
          <input :value="store.domain" type="text" readonly />
        </Field>
        <Field :label="t('settings.drawer.fields.profilePath')" :hint="t('settings.drawer.fields.profilePathHint')">
          <input value="/.well-known/ucp" type="text" readonly />
        </Field>
        <div class="kv-card">
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.discoverAt') }}</div>
            <div class="v mono break">{{ store.domain }}/.well-known/ucp</div>
          </div>
        </div>
      </template>

      <!-- ── return ── -->
      <template v-else-if="panel === 'return'">
        <Field :label="t('settings.drawer.fields.returnPolicyUrl')" :hint="t('settings.drawer.fields.returnPolicyHint')">
          <input v-model="returnPolicyUrl" type="url" :placeholder="t('settings.drawer.fields.returnPolicyPlaceholder')" @keydown.enter="onSaveReturn" />
        </Field>
        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.returnNote') }}
          </span>
        </div>
      </template>

      <!-- ── connector ── -->
      <template v-else-if="panel === 'connector'">
        <div v-if="connector" class="kv-card">
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.plugin') }}</div>
            <div class="v">
              <Chip :kind="connector.status === 'healthy' ? 'ok' : connector.status === 'pairing' ? 'quiet' : 'warn'">
                {{ connector.status }}
              </Chip>
            </div>
          </div>
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.pluginVersion') }}</div>
            <div class="v mono">{{ connector.plugin_version ?? '—' }}</div>
          </div>
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.syncModel') }}</div>
            <div class="v mono">{{ connector.sync_model ?? '—' }}</div>
          </div>
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.lastHeartbeat') }}</div>
            <div class="v">{{ formatFriendlyDateTime(connector.last_heartbeat_at) }}</div>
          </div>
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.lastSignal') }}</div>
            <div class="v">{{ formatFriendlyDateTime(connector.last_signal_at) }}</div>
          </div>
          <div class="kv-row">
            <div class="k">{{ t('settings.drawer.fields.lastFullSync') }}</div>
            <div class="v">{{ formatFriendlyDateTime(connector.last_full_sync_at) }}</div>
          </div>
        </div>
        <div v-else class="note">
          <Icon name="info" />
          <span>{{ t('settings.drawer.fields.noConnector') }}</span>
        </div>
        <div class="note">
          <Icon name="shield" />
          <span>
            {{ t('settings.drawer.fields.connectorNote') }}
          </span>
        </div>
      </template>

      <!-- ── handlers ── -->
      <template v-else-if="panel === 'handlers'">
        <div class="kv-card">
          <div
            v-for="(h, i) in payment.config?.handlers ?? []"
            :key="h.handler_id"
            class="handler-row"
            :class="{ first: i === 0 }"
          >
            <div class="handler-main">
              <div class="handler-id">{{ h.handler_id }}</div>
              <div class="handler-sub">
                <span class="mono">{{ h.name }}</span>
                ·
                <Chip
                  :kind="h.config_status === 'ready' ? 'ok' : h.config_status === 'incomplete' ? 'warn' : 'quiet'"
                >
                  {{ h.config_status }}
                </Chip>
              </div>
            </div>
            <Switch
              :model-value="!!handlerEnabled[h.handler_id ?? '']"
              :label="t('settings.drawer.fields.enableHandler', { handlerId: h.handler_id })"
              @update:model-value="(v) => (handlerEnabled[h.handler_id ?? ''] = v)"
            />
          </div>
        </div>
        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.handlersNote') }}
          </span>
        </div>
      </template>

      <!-- ── profile ── -->
      <template v-else-if="panel === 'profile'">
        <Field :label="t('settings.drawer.fields.legalBusinessName')" :hint="t('settings.drawer.fields.legalBusinessNameHint')">
          <input v-model="legalName" type="text" :placeholder="t('settings.drawer.fields.legalBusinessNamePlaceholder')" />
        </Field>
        <Field :label="t('settings.drawer.fields.supportEmail')" :hint="t('settings.drawer.fields.supportEmailHint')">
          <input v-model="supportEmail" type="email" :placeholder="t('settings.drawer.fields.supportEmailPlaceholder')" />
        </Field>
        <Field :label="t('settings.drawer.fields.storefrontDomain')" :hint="t('settings.drawer.fields.storefrontDomainHint')">
          <input v-model="profileDomain" type="url" :placeholder="t('settings.drawer.fields.storefrontDomainPlaceholder')" />
        </Field>
        <Field :label="t('settings.drawer.fields.fulfillmentRegions')" :hint="t('settings.drawer.fields.fulfillmentRegionsHint')">
          <select v-model="fulfillmentRegions" multiple size="6">
            <option v-for="c in localeCountries" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
        </Field>
        <div
          v-if="storeProfile.profile?.shipping_zones && Object.keys(storeProfile.profile.shipping_zones).length"
          class="kv-card"
        >
          <div
            v-for="(z, id, i) in storeProfile.profile.shipping_zones"
            :key="id"
            class="kv-row"
            :class="{ first: i === 0 }"
          >
            <div class="k">{{ z.name || id }}</div>
            <div class="v">{{ t('settings.drawer.fields.ratesCount', { count: (z.rates ?? []).length }) }}</div>
          </div>
        </div>
        <div class="note">
          <Icon name="info" />
          <span>
            {{ t('settings.drawer.fields.shippingNote') }}
          </span>
        </div>
      </template>
    </div>

    <!-- Disconnect confirmation dialog -->
    <div v-if="confirmDisconnect && panel === 'connector'" class="confirmation-overlay">
      <div class="confirmation-dialog">
        <div class="confirmation-title">{{ t('settings.drawer.confirmDisconnect.title') }}</div>
        <div class="confirmation-message">
          {{ t('settings.drawer.confirmDisconnect.message') }}
        </div>
        <div class="confirmation-buttons">
          <button class="btn btn-ghost" type="button" @click="confirmDisconnect = false">{{ t('settings.drawer.footer.cancel') }}</button>
          <button
            class="btn btn-danger"
            type="button"
            :disabled="submitting"
            @click="onDisconnect"
          >
            {{ submitting ? t('settings.drawer.confirmDisconnect.disconnecting') : t('settings.drawer.confirmDisconnect.disconnect') }}
          </button>
        </div>
      </div>
    </div>

    <div class="drawer-foot">
      <template v-if="panel === 'name'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.cancel') }}</button>
        <button class="btn btn-primary" type="button" :disabled="!name.trim() || submitting" @click="onSaveName">
          {{ submitting ? t('settings.drawer.footer.saving') : t('settings.drawer.footer.save') }}
        </button>
      </template>
      <template v-else-if="panel === 'locale'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.cancel') }}</button>
        <button class="btn btn-primary" type="button" :disabled="submitting" @click="onSaveLocale">
          {{ submitting ? t('settings.drawer.footer.saving') : t('settings.drawer.footer.save') }}
        </button>
      </template>
      <template v-else-if="panel === 'return'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.cancel') }}</button>
        <button class="btn btn-primary" type="button" :disabled="submitting" @click="onSaveReturn">
          {{ submitting ? t('settings.drawer.footer.saving') : t('settings.drawer.footer.save') }}
        </button>
      </template>
      <template v-else-if="panel === 'plan' || panel === 'payment' || panel === 'invoices'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.close') }}</button>
        <button class="btn btn-primary" type="button" @click="onOpenPortal">
          <Icon name="arrowR" /> {{ t('settings.drawer.footer.openStripePortal') }}
        </button>
      </template>
      <template v-else-if="panel === 'connector'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.close') }}</button>
        <div class="btn-group" v-if="connector">
          <button
            class="btn btn-secondary"
            type="button"
            :disabled="submitting"
            @click="confirmDisconnect = true"
          >
            <Icon name="x" /> {{ t('settings.drawer.confirmDisconnect.disconnect') }}
          </button>
          <button
            class="btn btn-primary"
            type="button"
            :disabled="submitting"
            @click="onResync"
          >
            <Icon name="zap" /> {{ submitting ? t('settings.drawer.footer.queuing') : t('settings.drawer.footer.triggerFullSync') }}
          </button>
        </div>
      </template>
      <template v-else-if="panel === 'handlers'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.cancel') }}</button>
        <button class="btn btn-primary" type="button" :disabled="submitting" @click="onSaveHandlers">
          {{ submitting ? t('settings.drawer.footer.saving') : t('settings.drawer.footer.save') }}
        </button>
      </template>
      <template v-else-if="panel === 'profile'">
        <button class="btn btn-ghost" type="button" @click="emit('close')">{{ t('settings.drawer.footer.cancel') }}</button>
        <button class="btn btn-primary" type="button" :disabled="submitting" @click="onSaveProfile">
          {{ submitting ? t('settings.drawer.footer.saving') : t('settings.drawer.footer.save') }}
        </button>
      </template>
      <template v-else>
        <button class="btn btn-ghost block" type="button" @click="emit('close')">{{ t('settings.drawer.footer.close') }}</button>
      </template>
    </div>
  </aside>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  background: color-mix(in oklab, var(--ink) 32%, transparent);
  backdrop-filter: blur(2px);
  z-index: 60;
  animation: fade 0.2s both;
}
@keyframes fade { from { opacity: 0; } to { opacity: 1; } }

.drawer {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(540px, 96vw);
  background: var(--bg);
  z-index: 61;
  box-shadow: -20px 0 60px -20px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  animation: slidein 0.32s cubic-bezier(0.22, 0.9, 0.3, 1) both;
}
@keyframes slidein { from { transform: translateX(100%); } to { transform: none; } }

.drawer-head {
  padding: 22px 26px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.head-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.title { font-size: 22px; }
.sub { color: var(--muted); font-size: 13.5px; margin-top: 6px; max-width: 42ch; }

.icon-btn {
  width: 33px;
  height: 33px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--muted);
  background: none;
  border: 0;
  cursor: pointer;
}
.icon-btn:hover { background: var(--surface2); color: var(--ink); }

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 24px 26px 40px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.drawer-foot {
  padding: 16px 26px;
  border-top: 1px solid var(--line);
  background: var(--surface);
  display: flex;
  gap: 10px;
}
.btn {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border-radius: var(--radius-sm);
  padding: 11px 16px;
  font-weight: 600;
  font-size: var(--fs-small);
  border: 0;
  cursor: pointer;
}
.btn :deep(svg) { width: 15px; height: 15px; }
.btn.block { width: 100%; }
.btn-primary { background: var(--accent); color: #fff; }
.btn-primary:hover:not(:disabled) { background: var(--accent-deep); }
.btn-primary:disabled { opacity: 0.45; cursor: default; }
.btn-ghost {
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
}
.btn-ghost:hover { box-shadow: inset 0 0 0 1px var(--faint); }

.kv-card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  overflow: hidden;
}
.kv-row {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 18px;
  border-top: 1px solid var(--line);
}
.kv-row:first-child { border-top: 0; }
.k { color: var(--muted); font-size: 13px; }
.v { color: var(--ink); font-size: 13.5px; font-weight: 500; text-align: right; }
.v.break { word-break: break-all; }

.note {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 13px 15px;
  background: var(--accent-softer);
  border: 1px solid var(--accent-line);
  border-radius: var(--radius-sm);
  font-size: 12.5px;
  color: var(--muted);
  line-height: 1.5;
}
.note :deep(svg) { width: 15px; height: 15px; color: var(--accent); flex-shrink: 0; margin-top: 1px; }
.note strong { color: var(--ink); font-weight: 600; }

.note-error {
  background: color-mix(in oklab, #d32f2f 10%, var(--surface));
  border-color: color-mix(in oklab, #d32f2f 35%, var(--line));
}
.note-error :deep(svg) { color: #d32f2f; }

/* Team */
.team-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 18px;
  border-top: 1px solid var(--line);
}
.team-row.first { border-top: 0; }
.avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--surface2);
  color: var(--muted);
  font: 600 11px/1 var(--font-ui);
  display: grid;
  place-items: center;
  border: 1px solid var(--line);
  flex-shrink: 0;
}
.team-main { flex: 1; min-width: 0; }
.team-email { font-weight: 500; font-size: 13.5px; }
.team-chips { display: flex; gap: 6px; flex-shrink: 0; }

.invite-row {
  display: flex;
  gap: 8px;
}
.invite-row input { flex: 1; }
.invite-row .btn { flex: none; padding-inline: 18px; }

/* Plan — display-only cards; switching happens in the Stripe portal */
.plan-opt {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 13px;
  padding: 15px 17px;
  border: 1px solid var(--line2);
  border-radius: var(--radius);
  transition: border-color 0.15s, background 0.15s;
  width: 100%;
  text-align: left;
  background: var(--surface);
  color: inherit;
  font: inherit;
}
.plan-opt.sel { border-color: var(--accent); background: var(--accent-softer); }
.plan-main { flex: 1; min-width: 0; }
.plan-top {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 10px;
}
.plan-name { font-weight: 600; }
.plan-price-wrap { white-space: nowrap; }
.plan-price { font-weight: 600; }
.plan-cadence { color: var(--faint); font-size: 12.5px; margin-left: 4px; }
.plan-blurb {
  display: block;
  color: var(--muted);
  font-size: 13px;
  margin-top: 5px;
  line-height: 1.5;
}

/* Handlers */
.handler-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-top: 1px solid var(--line);
}
.handler-row.first { border-top: 0; }
.handler-main { flex: 1; min-width: 0; }
.handler-id { font-weight: 600; }
.handler-sub {
  color: var(--muted);
  font-size: 13px;
  margin-top: 4px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* Field-style readonly inputs */
input[readonly] { background: var(--surface2); color: var(--ink); cursor: default; }

/* Button group layout */
.btn-group {
  display: flex;
  gap: 10px;
}
.btn-group .btn { flex: 1; }

/* Secondary button style */
.btn-secondary {
  background: var(--surface2);
  box-shadow: inset 0 0 0 1px var(--line2);
  color: var(--ink);
}
.btn-secondary:hover:not(:disabled) { background: var(--surface2); box-shadow: inset 0 0 0 1px var(--faint); }
.btn-secondary:disabled { opacity: 0.45; cursor: default; }

/* Danger button style */
.btn-danger {
  background: #d32f2f;
  color: #fff;
}
.btn-danger:hover:not(:disabled) { background: #b71c1c; }
.btn-danger:disabled { opacity: 0.45; cursor: default; }

/* Confirmation dialog overlay */
.confirmation-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 62;
}

.confirmation-dialog {
  background: var(--bg);
  border-radius: var(--radius);
  border: 1px solid var(--line);
  padding: 24px;
  max-width: 400px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.confirmation-title {
  font-weight: 600;
  font-size: 16px;
  color: var(--ink);
}

.confirmation-message {
  font-size: 14px;
  color: var(--muted);
  line-height: 1.5;
}

.confirmation-buttons {
  display: flex;
  gap: 10px;
  margin-top: 12px;
}

.confirmation-buttons .btn {
  flex: 1;
}
</style>
