<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Sets the operator's enter-as-merchant context, then bounces to /path so
// the merchant surfaces render. The OperatorBar in AppShell stays visible
// for the duration. Exit clears the context and returns to /board.

import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useOperatorStore } from '@/stores/operator'

const props = defineProps<{ merchantId: string; storeId: string }>()

const operator = useOperatorStore()
const router = useRouter()

onMounted(() => {
  operator.enterMerchant(
    props.merchantId,
    props.storeId,
    operator.enterAs?.merchantName,
    operator.enterAs?.storeName,
  )
  router.replace('/path')
})
</script>

<template>
  <div />
</template>
