// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Which named task gets a bespoke widget instead of the generic TaskCard.
//
// Everything on the Path is a task — that is the spine model. These widgets
// are *presentation* for the handful of tasks the merchant meets at a known
// point in every store's journey, where a generic card would waste the
// chance to show the thing that actually matters (how many products landed,
// what the audit scored, how long Google has had it).
//
// Matching keys off `source` + `source_ref` only. Both are server-derived
// spine fields (sarvex-v1-control-plane.yaml `task`), and `source_ref` is
// the stable identity the backend builds task ids from (task.deriveTaskID).
// Never match on task_id: it embeds the store id and is not stable across
// stores.
//
// The milestone refs below are raised by the journey state machine
// (internal/planes/control/journey/stage_work.go, where `stages` is the one
// table of stage-prescribed work). Every other task renders through
// TaskCard. Widening a matcher is how you accidentally swallow one of
// those, so each is written to match exactly one milestone.
//
// Checklist items (gpay-* included) used to get a bespoke widget here —
// GooglePayTask — because GET /tasks/{id} had no drill-in content for
// source=checklist at all, so the drawer had nothing to show. TaskDrawer.vue
// now reads the checklist store directly for any checklist task's
// instructions_url, which is the fix every OTHER checklist item (mc-*,
// waitlist-form, cred-sandbox, attr-eligibility) already relied on having.
// Keeping GooglePayTask around after that meant its two items were the only
// ones with an instructions link and a step counter on the *card* as well
// as the drawer, and a "Needs you" chip the generic card doesn't show for
// guided/open tasks — a real, user-visible inconsistency, not a feature.

import type { Component } from 'vue'
import type { components } from '@/api/schema'
import CatalogSyncTask from './CatalogSyncTask.vue'
import CatalogAuditTask from './CatalogAuditTask.vue'
import PreValidationTask from './PreValidationTask.vue'

type Task = components['schemas']['task']

export type TaskWidgetId = 'catalog_sync' | 'catalog_audit' | 'pre_validation'

type WidgetEntry = {
  component: Component
  matches: (task: Task) => boolean
}

export const TASK_WIDGETS: Record<TaskWidgetId, WidgetEntry> = {
  // Two shapes share source=catalog_import: the journey's in-progress
  // milestone (owner=sarvex) and the sync plane's import-failure task
  // (owner=merchant, raiseImportFailedTask). The widget renders both.
  catalog_sync: {
    component: CatalogSyncTask,
    matches: (t) => t.source === 'catalog_import',
  },

  // source=readiness is otherwise per-SKU findings, which keep the generic
  // card and its remediation drill-in — so this matches the milestone ref
  // exactly, not the source.
  catalog_audit: {
    component: CatalogAuditTask,
    matches: (t) => t.source === 'readiness' && t.source_ref === 'initial_audit',
  },

  // Sarvex's own pre-validation milestone (source_ref=run) and its failure
  // (source_ref=pre_validation_failed) — matched exactly (not just
  // source=validation). The refs are fixed server-side in
  // internal/planes/control/validation. External sandbox/production
  // verdicts (source_ref=sandbox/production) are not raised, so no widget
  // matches them.
  pre_validation: {
    component: PreValidationTask,
    matches: (t) => t.source === 'validation' && (t.source_ref === 'run' || t.source_ref === 'pre_validation_failed'),
  },
}

/**
 * The widget that should render `task`, or null when nothing named matches
 * and the generic TaskCard should be used.
 */
export function widgetForTask(task: Task): Component | null {
  for (const entry of Object.values(TASK_WIDGETS)) {
    if (entry.matches(task)) return entry.component
  }
  return null
}
