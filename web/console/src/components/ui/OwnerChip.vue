<!--
  Copyright 2026 Sarvex Lab
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Owner chip. Takes the OpenAPI task_owner enum directly and maps it to the
// design's visual `you` / `us` / `external` styles + labels.

import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { components } from '@/api/schema'
import Chip from './Chip.vue'
import Icon, { type IconName } from './Icon.vue'

type Owner = components['schemas']['task_owner']

const props = defineProps<{ owner: Owner }>()

const { t } = useI18n()

type ChipKind = 'you' | 'us' | 'external'
type View = { kind: ChipKind; icon: IconName }

const map: Record<Owner, View> = {
  merchant: { kind: 'you',    icon: 'user'   },
  sarvex:   { kind: 'us',     icon: 'spark'  },
  external:   { kind: 'external', icon: 'external' },
}

const view = computed(() => map[props.owner])
const label = computed(() => t(`common.owner.${props.owner}`))
</script>

<template>
  <Chip :kind="view.kind">
    <Icon :name="view.icon" />{{ label }}
  </Chip>
</template>
