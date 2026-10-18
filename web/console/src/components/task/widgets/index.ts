// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Named task widgets — bespoke rendering for the handful of spine tasks the
// merchant meets at a known point in every store's journey. See
// ./registry.ts for how they bind to tasks and why every other task
// deliberately keeps the generic card.

export { default as CatalogSyncTask } from './CatalogSyncTask.vue'
export { default as CatalogAuditTask } from './CatalogAuditTask.vue'
export { default as WorkingBar } from './WorkingBar.vue'

export { TASK_WIDGETS, widgetForTask, type TaskWidgetId } from './registry'
