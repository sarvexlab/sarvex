// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Pins which tasks are *named* (get a widget) and which stay generic.
//
// The important half is the negative half. The spine is open — most tasks
// are created at runtime from findings — and they must keep rendering
// through TaskCard. Several matchers share a `source` with those runtime
// tasks (readiness findings), so a matcher widened to the source alone
// would silently swallow them and lose their drill-in. Every checklist
// item, gpay-* included, is generic now — see registry.ts's doc comment
// for why gpay-* isn't a widget anymore.

import { describe, it, expect } from 'vitest'
import { widgetForTask, TASK_WIDGETS } from './registry'
import CatalogSyncTask from './CatalogSyncTask.vue'
import CatalogAuditTask from './CatalogAuditTask.vue'
import PreValidationTask from './PreValidationTask.vue'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']

const task = (over: Partial<Task> = {}): Task => ({
  task_id: 't',
  store_id: 's1',
  title: 'A task',
  owner: 'merchant',
  resolution: 'guided',
  state: 'open',
  source: 'readiness',
  source_ref: 'sku-1',
  ...over,
})

describe('widgetForTask — the journey milestones', () => {
  it.each([
    ['catalog import', { source: 'catalog_import' as const, source_ref: 'initial_import' }, CatalogSyncTask],
    ['catalog import failure', { source: 'catalog_import' as const, source_ref: 'initial' }, CatalogSyncTask],
    ['readiness audit', { source: 'readiness' as const, source_ref: 'initial_audit' }, CatalogAuditTask],
    ["Sarvex's own pre-validation", { source: 'validation' as const, source_ref: 'run' }, PreValidationTask],
    ['a pre-validation failure', { source: 'validation' as const, source_ref: 'pre_validation_failed' }, PreValidationTask],
  ])('renders %s through its widget', (_label, over, expected) => {
    expect(widgetForTask(task(over))).toBe(expected)
  })
})

describe('widgetForTask — everything else stays generic', () => {
  it.each([
    ['a per-SKU readiness finding', { source: 'readiness' as const, source_ref: 'HP-1042' }],
    ['a monitoring finding', { source: 'monitoring' as const, source_ref: 'price' }],
    ['a consistency finding', { source: 'consistency' as const, source_ref: 'stock' }],
    ['a checklist item', { source: 'checklist' as const, source_ref: 'mc-shipping' }],
    ['google pay setup', { source: 'checklist' as const, source_ref: 'gpay-setup' }],
    ['google pay merchant id', { source: 'checklist' as const, source_ref: 'gpay-merchantid' }],
    // Sandbox/production verdicts are external; the journey does not raise
    // these refs, so they stay generic.
    ['a sandbox verdict', { source: 'validation' as const, source_ref: 'sandbox' }],
    ['a go-live review', { source: 'validation' as const, source_ref: 'production' }],
    ['a task with no source_ref', { source: 'readiness' as const, source_ref: null }],
  ])('leaves %s to the generic card', (_label, over) => {
    expect(widgetForTask(task(over))).toBeNull()
  })

  it('does not confuse a pre-validation run with its failure counterpart', () => {
    // Both are source=validation, matched exactly by source_ref so neither
    // is ever conflated with the other.
    expect(widgetForTask(task({ source: 'validation', source_ref: 'run' }))).toBe(
      PreValidationTask,
    )
    expect(widgetForTask(task({ source: 'validation', source_ref: 'pre_validation_failed' }))).toBe(
      PreValidationTask,
    )
  })
})

describe('TASK_WIDGETS', () => {
  it('matches on source/source_ref only — never on the store-scoped task id', () => {
    // deriveTaskID embeds the store id, so a matcher keyed off task_id would
    // work on one store and silently fail on the next.
    for (const [id, entry] of Object.entries(TASK_WIDGETS)) {
      const a = entry.matches(task({ task_id: 'validation:store-a:run', source: 'validation', source_ref: 'run' }))
      const b = entry.matches(task({ task_id: 'validation:store-b:run', source: 'validation', source_ref: 'run' }))
      expect(a, id).toBe(b)
    }
  })

  it('covers every milestone the journey raises', () => {
    // Mirrors `stages` in internal/planes/control/journey/stage_work.go —
    // if a milestone is added there without a widget here it renders
    // generically, which is safe but loses the enrichment.
    const refs = [
      { source: 'catalog_import' as const, source_ref: 'initial_import' },
      { source: 'readiness' as const, source_ref: 'initial_audit' },
    ]
    for (const r of refs) {
      expect(widgetForTask(task(r)), r.source_ref).not.toBeNull()
    }
  })
})
