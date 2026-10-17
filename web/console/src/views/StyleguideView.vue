<!--
  Copyright 2026 Sarvex Labs
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
// Scratch route that renders every shared component against representative
// fixtures so they can be reviewed in both themes. Lives at /_/styleguide
// and is not linked from the nav — pure development surface.

import { ref } from 'vue'
import type { components } from '@/api/schema'
import Chip from '@/components/ui/Chip.vue'
import OwnerChip from '@/components/ui/OwnerChip.vue'
import Field from '@/components/ui/Field.vue'
import Icon from '@/components/ui/Icon.vue'
import JourneyBar from '@/components/journey/JourneyBar.vue'
import TaskCard from '@/components/task/TaskCard.vue'
import { widgetForTask } from '@/components/task/widgets'
import StatTile from '@/components/journey/stats/StatTile.vue'

type Task = components['schemas']['task']
type State = components['schemas']['ucp_state']

const journeyCurrent = ref<State>('profile_published')
const journeyPreview = ref<State | null>(null)

const tasks: Task[] = [
  {
    task_id: 't-1',
    title: 'Pick a primary brand colour',
    why_it_matters: "Used in the agent storefront card and in checkout.",
    owner: 'merchant',
    resolution: 'guided',
    state: 'open',
    blocks_checkout: false,
    source: 'checklist',
    source_ref: null,
    created_at: '2026-06-22T10:00:00Z',
    updated_at: '2026-06-22T10:00:00Z',
  },
  {
    task_id: 't-2',
    title: 'Map 12 shipping classes to Google taxonomy',
    why_it_matters: 'Agents quote shipping per region from this mapping.',
    owner: 'merchant',
    resolution: 'one_click',
    state: 'open',
    blocks_checkout: true,
    source: 'readiness',
    source_ref: null,
    created_at: '2026-06-22T10:00:00Z',
    updated_at: '2026-06-22T10:00:00Z',
  },
  {
    task_id: 't-3',
    title: 'Reconciling catalog updates',
    why_it_matters: 'Catching up the serving cache after a large sync.',
    owner: 'sarvex',
    resolution: 'automatic',
    state: 'in_progress',
    blocks_checkout: false,
    source: 'monitoring',
    source_ref: null,
    created_at: '2026-06-22T10:00:00Z',
    updated_at: '2026-06-22T10:00:00Z',
  },
  {
    task_id: 't-4',
    title: 'Sandbox validation under review',
    why_it_matters: "An external party is reviewing your submission; we'll email when it clears.",
    owner: 'external',
    resolution: 'external_wait',
    state: 'blocked',
    blocks_checkout: false,
    source: 'validation',
    source_ref: null,
    created_at: '2026-06-22T10:00:00Z',
    updated_at: '2026-06-22T10:00:00Z',
  },
  {
    task_id: 't-5',
    title: 'Set business return policy URL',
    why_it_matters: 'Agents link shoppers to it when they ask about returns.',
    owner: 'merchant',
    resolution: 'guided',
    state: 'done',
    blocks_checkout: false,
    source: 'checklist',
    source_ref: null,
    created_at: '2026-06-22T10:00:00Z',
    updated_at: '2026-06-22T10:00:00Z',
  },
]

const lastEvent = ref<string>('')
// The milestone tasks the journey raises, plus a checklist-derived one —
// shown here as they arrive from the spine, resolved through the same
// registry TaskFeed uses.
const milestoneTasks: Task[] = [
  {
    task_id: 'catalog_import:demo:initial_import',
    title: 'Importing your catalog',
    why_it_matters: "We're reading your products out of your store so agents can see them.",
    owner: 'sarvex', resolution: 'automatic', state: 'in_progress',
    source: 'catalog_import', source_ref: 'initial_import',
  },
  {
    task_id: 'readiness:demo:initial_audit',
    title: 'Auditing your catalog',
    why_it_matters: 'Checking completeness, identifiers, categories, and policy facts.',
    owner: 'sarvex', resolution: 'automatic', state: 'in_progress',
    source: 'readiness', source_ref: 'initial_audit',
  },
  {
    task_id: 'checklist:demo:gpay-setup',
    title: 'Complete Google Pay & Wallet Console setup',
    why_it_matters: 'A verified merchant account is required before checkout can go live.',
    owner: 'merchant', resolution: 'guided', state: 'open',
    source: 'checklist', source_ref: 'gpay-setup',
  },
  {
    task_id: 'validation:demo:sandbox',
    title: 'Sandbox checkout validation',
    why_it_matters: 'Test purchases run against your catalog. Typical wait: 3–5 business days.',
    owner: 'external', resolution: 'external_wait', state: 'blocked',
    source: 'validation', source_ref: 'sandbox',
  },
  {
    task_id: 'validation:demo:production',
    title: 'Final go-live review',
    why_it_matters: "Last checks before your Buy button switches on.",
    owner: 'external', resolution: 'external_wait', state: 'blocked',
    source: 'validation', source_ref: 'production',
  },
]

function widgetFor(t: Task) {
  return widgetForTask(t) ?? TaskCard
}

function onOpen(t: Task) { lastEvent.value = `open → ${t.task_id}` }
function onOp(action: string, t: Task) { lastEvent.value = `op ${action} → ${t.task_id}` }
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div class="eyebrow">Internal · Styleguide</div>
      <h1 class="page-title display">Shared components</h1>
      <p class="page-sub">
        The primitives every surface composes. Toggle the topbar's theme to verify
        each in paper and midnight. Not linked from the nav.
      </p>
    </div>

    <section>
      <h2 class="display section">Chip</h2>
      <div class="row">
        <Chip kind="you">You</Chip>
        <Chip kind="us">Sarvex</Chip>
        <Chip kind="external">Google</Chip>
        <Chip kind="ok"><Icon name="check" /> Ready</Chip>
        <Chip kind="warn">Warning</Chip>
        <Chip kind="danger">Blocked</Chip>
        <Chip kind="quiet"><Icon name="clock" /> Waiting</Chip>
      </div>
    </section>

    <section>
      <h2 class="display section">OwnerChip (typed on task_owner)</h2>
      <div class="row">
        <OwnerChip owner="merchant" />
        <OwnerChip owner="sarvex" />
        <OwnerChip owner="external" />
      </div>
    </section>

    <section>
      <h2 class="display section">Field</h2>
      <div class="card field-host">
        <Field label="Business name" hint="Used in invoices and the storefront card.">
          <input placeholder="Acme Tea Co." />
        </Field>
        <Field label="Return window (days)">
          <input type="number" value="30" />
        </Field>
      </div>
    </section>

    <section>
      <h2 class="display section">JourneyBar</h2>
      <div class="stack">
        <JourneyBar :current="journeyCurrent" />
        <JourneyBar
          :current="journeyCurrent"
          :selected="journeyPreview"
          selectable
          @select="(s) => (journeyPreview = s)"
        />
        <p class="muted">Selectable · selected = {{ journeyPreview ?? 'current' }}</p>
      </div>
    </section>

    <section>
      <h2 class="display section">TaskCard</h2>
      <div class="task-feed">
        <TaskCard
          v-for="t in tasks"
          :key="t.task_id"
          :task="t"
          @open="onOpen"
          @op="onOp"
        />
      </div>
    </section>

    <section>
      <h2 class="display section">TaskCard · operator</h2>
      <div class="task-feed">
        <TaskCard
          v-for="t in tasks.slice(0, 3)"
          :key="t.task_id"
          :task="t"
          as-operator
          store-name="Acme Tea Co."
          @open="onOpen"
          @op="onOp"
        />
      </div>
    </section>

    <section>
      <h2 class="display section">Named task widgets</h2>
      <p class="muted">
        Milestone tasks the journey raises, each resolved to its widget through the same registry
        TaskFeed uses. They enrich the spine with data it has no field for (product counts, audit
        score, submission time), so what they show reflects the current store. Every other task
        renders through TaskCard above.
      </p>
      <div class="task-feed widgets">
        <component
          :is="widgetFor(t)"
          v-for="t in milestoneTasks"
          :key="t.task_id"
          :task="t"
          @open="onOpen"
        />
      </div>
    </section>

    <section>
      <h2 class="display section">Stage stat tiles</h2>
      <p class="muted">
        Facts, not work — deliberately not task cards, so a metric never reads as something the
        merchant has to act on.
      </p>
      <div class="stat-grid">
        <StatTile value="Connected" tone="ok" label="WooCommerce" note="Last signal 2 minutes ago" />
        <StatTile value="38" tone="ok" label="Agent orders this week" note="$2,140.00 in agent-driven sales." />
        <StatTile value="In sync" tone="ok" label="Prices &amp; inventory" note="Last checked 2 minutes ago." />
        <StatTile value="3" tone="warn" label="Products drifted across sources" note="Each drifted product is its own task below." />
      </div>
    </section>

    <p v-if="lastEvent" class="muted">Last event: <code>{{ lastEvent }}</code></p>
  </div>
</template>

<style scoped>
.page { max-width: 1080px; margin: 0 auto; padding: 40px 26px 130px; }
.page-head { margin-bottom: 30px; }
.page-title { font-size: 34px; }
.page-sub { color: var(--muted); font-size: 16px; margin-top: 8px; max-width: 60ch; }

section { margin-top: 38px; }
.section { font-size: 18px; margin-bottom: 14px; }

.row { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.stack { display: flex; flex-direction: column; gap: 14px; }
.muted { color: var(--muted); }
.task-feed { display: flex; flex-direction: column; gap: var(--gap-feed); }
.widgets { margin-top: 14px; }
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
  margin-top: 14px;
}

.field-host {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  padding: 22px 24px;
  max-width: 480px;
  box-shadow: var(--shadow);
}

code {
  background: var(--surface2);
  padding: 2px 7px;
  border-radius: 6px;
  font-family: var(--font-mono);
  font-size: 12.5px;
}
</style>
