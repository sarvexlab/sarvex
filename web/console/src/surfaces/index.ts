// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Per-surface console modules.
//
// A surface's copy lives in src/surfaces/<surface_id>/locale.en.ts and is
// picked up here by glob: adding a surface adds a folder, and nothing shared —
// not en.ts, not the router, not the store, not the enrollment component —
// is edited. The folder name must equal the descriptor's surface_id; the
// backend's descriptor then supplies every other fact (checks, artifacts,
// steps, config fields) and this folder supplies only its words.
//
// Anything a surface needs beyond words (a bespoke artifact view, say) is
// registered by artifact id in the same folder's index — see
// components/surface/artifactViews.ts. No such view exists yet: the generic
// artifact card renders the artifact's URL.

type Messages = Record<string, unknown>

const modules = import.meta.glob<{ default: Messages }>('./*/locale.en.ts', { eager: true })

/** `{ [surface_id]: messages }` for every surface folder. */
export const surfaceMessages: Record<string, Messages> = Object.fromEntries(
  Object.entries(modules).map(([path, mod]) => {
    const id = path.split('/')[1]
    return [id, mod.default]
  }),
)
