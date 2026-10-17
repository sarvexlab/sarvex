<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useStoresStore } from '@/stores/stores'
import { selectedMode } from '@/auth/provider'
import { fetchMerchantOptions, type MerchantOption } from '@/auth/simulator'
import { apiMode } from '@/api/mocks'
import Mark from '@/components/shell/Mark.vue'

const auth = useAuthStore()
const stores = useStoresStore()
const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const useSimulator = selectedMode() === 'simulator'
const role = ref<'owner' | 'staff' | 'operator'>('owner')
const submitting = ref(false)

// Merchant picker — only relevant for owner/staff in simulator mode.
const merchants = ref<MerchantOption[]>([])
const merchantsLoading = ref(false)
const merchantsError = ref<string | null>(null)
const selectedMerchantId = ref<string | null>(null)

const selectedMerchant = computed(
  () => merchants.value.find((m) => m.merchantId === selectedMerchantId.value) ?? null,
)
const needsMerchantPicker = computed(() => useSimulator && role.value !== 'operator')
// Once the merchant fetch fails in real mode, signIn() would just throw the
// same failure again — no fallback identity to fall back to (see
// simulator.ts). Block the button so we show one error, not two.
const isConnectionError = computed(
  () => !!merchantsError.value && apiMode('/operator/merchants') === 'real',
)

async function loadMerchants() {
  // Clear first: a stale picker error must not block operator sign-in,
  // which needs no merchant (isConnectionError disables the button).
  merchantsError.value = null
  if (!needsMerchantPicker.value) return
  merchantsLoading.value = true
  try {
    merchants.value = await fetchMerchantOptions()
    if (merchants.value.length === 1) {
      selectedMerchantId.value = merchants.value[0].merchantId
    }
  } catch (e) {
    merchantsError.value =
      apiMode('/operator/merchants') === 'real'
        ? t('login.connectionError')
        : t('login.merchantsLoadError')
  } finally {
    merchantsLoading.value = false
  }
}

// Reload merchant list when switching between owner/staff and operator.
watch(
  () => role.value,
  () => {
    selectedMerchantId.value = null
    merchants.value = []
    loadMerchants()
  },
  { immediate: true },
)

// Push selected merchant into window so signIn() can read it.
watch(selectedMerchant, (m) => {
  ;(window as unknown as { __sarvexSimMerchant?: MerchantOption }).__sarvexSimMerchant =
    m ?? undefined
})

const buttonLabel = computed(() => {
  if (submitting.value) return t('login.signingIn')
  return useSimulator ? t('login.continueWithGoogle') : t('login.continueButton')
})

const canSubmit = computed(() => {
  if (submitting.value) return false
  // Real mode has no fallback identity (see simulator.ts), so owner/staff
  // sign-in needs a picked merchant — including when the list is empty.
  // Mock mode falls back to default claims, so only a non-empty list forces a pick.
  if (needsMerchantPicker.value && !selectedMerchantId.value) {
    if (merchants.value.length > 0 || apiMode('/operator/merchants') === 'real') return false
  }
  if (isConnectionError.value) return false
  return true
})

// Once the principal lands, route by role.
watch(
  () => auth.isAuthenticated,
  async (ok) => {
    if (!ok) return
    const explicitNext = route.query.next as string | undefined
    if (explicitNext) {
      router.replace(explicitNext)
      return
    }
    if (auth.isOperator) {
      router.replace('/board')
      return
    }
    await stores.load()
    router.replace(stores.stores.length > 1 ? '/account' : '/path')
  },
  { immediate: true },
)

async function onSignIn() {
  submitting.value = true
  try {
    if (useSimulator) {
      ;(window as unknown as { __sarvexSimRole?: typeof role.value }).__sarvexSimRole = role.value
    }
    await auth.signIn()
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="login">
    <div class="card">
      <div class="wordmark"><Mark /> Sarvex</div>
      <h1 class="display">{{ t('login.signIn') }}</h1>
      <p class="muted">{{ t('login.useGoogleAccount') }}</p>

      <p v-if="auth.error" class="err" role="alert">{{ auth.error }}</p>

      <button class="primary" type="button" :disabled="!canSubmit" @click="onSignIn">
        {{ buttonLabel }}
      </button>
      <p v-if="!useSimulator" class="hint center">
        {{ t('login.ssoHint') }}
      </p>

      <div v-if="useSimulator" class="sim">
        <div class="sim-label">{{ t('login.simLabel') }}</div>
        <div class="seg">
          <button
            v-for="r in (['owner', 'staff', 'operator'] as const)"
            :key="r"
            :class="{ active: role === r }"
            type="button"
            @click="role = r"
          >
            {{ r }}
          </button>
        </div>

        <!-- Merchant picker: owner / staff only -->
        <template v-if="needsMerchantPicker">
          <div class="sim-label" style="margin-top: 6px">{{ t('login.merchant') }}</div>
          <div v-if="merchantsLoading" class="pick-loading">{{ t('login.loadingMerchants') }}</div>
          <div v-else-if="merchantsError" class="pick-error">{{ merchantsError }}</div>
          <div v-else-if="merchants.length === 0" class="pick-empty">
            {{ t('login.noMerchantsFound') }}
          </div>
          <div v-else class="pick-list">
            <button
              v-for="m in merchants"
              :key="m.merchantId"
              type="button"
              :class="['pick-item', { active: selectedMerchantId === m.merchantId }]"
              @click="selectedMerchantId = m.merchantId"
            >
              <span class="pick-name">{{ m.merchantName }}</span>
              <span class="pick-meta mono">
                {{ m.merchantId }}
                <template v-if="m.storeIds.length"> · {{ m.storeIds.length === 1 ? t('login.storeSingular', { count: m.storeIds.length }) : t('login.storePlural', { count: m.storeIds.length }) }}</template>
              </span>
            </button>
          </div>
          <p v-if="selectedMerchant" class="pick-owner">
            {{ t('login.ownerLabel') }} <span class="mono">{{ selectedMerchant.ownerEmail }}</span>
          </p>
        </template>

        <p class="hint">
          {{ t('login.simHint') }}
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  padding: 24px;
}
.card {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  padding: 32px 32px 28px;
  width: 100%;
  max-width: 420px;
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.wordmark {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: var(--font-display);
  font-weight: var(--display-weight);
  font-size: 20px;
  letter-spacing: -0.02em;
}
h1 { font-size: 26px; }
.muted { color: var(--muted); margin: 0; }
.err {
  background: color-mix(in oklab, var(--danger) 12%, var(--surface));
  color: var(--danger);
  border: 1px solid color-mix(in oklab, var(--danger) 35%, var(--line));
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  font-size: var(--fs-small);
}
.primary {
  margin-top: 4px;
  padding: 12px 16px;
  border-radius: var(--radius-sm);
  background: var(--accent);
  color: #fff;
  font-weight: 600;
}
.primary:disabled { opacity: 0.6; cursor: default; }

.sim {
  border-top: 1px dashed var(--line2);
  margin-top: 14px;
  padding-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.sim-label {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
}
.seg {
  display: flex;
  gap: 4px;
  padding: 4px;
  border-radius: 99px;
  background: var(--surface2);
}
.seg button {
  flex: 1;
  padding: 7px 10px;
  border-radius: 99px;
  font-size: 13px;
  font-weight: 500;
  color: var(--muted);
}
.seg button.active {
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow);
}
.hint {
  font-size: 12px;
  color: var(--faint);
  margin: 0;
  line-height: 1.5;
}
.hint.center { text-align: center; margin-top: 6px; }

/* Merchant picker */
.pick-loading,
.pick-empty {
  font-size: 12.5px;
  color: var(--faint);
  padding: 4px 0;
}
.pick-error {
  font-size: 12.5px;
  color: var(--danger);
}
.pick-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 180px;
  overflow-y: auto;
}
.pick-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface2);
  border: 1px solid transparent;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.1s;
}
.pick-item:hover { border-color: var(--line2); }
.pick-item.active {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--accent) 8%, var(--surface));
}
.pick-name { font-size: 13.5px; font-weight: 600; }
.pick-meta { font-size: 11.5px; color: var(--faint); }
.pick-owner { font-size: 12px; color: var(--muted); margin: 0; }
</style>
