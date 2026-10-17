// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// example_marketplace — a demo surface that exists only in the console's mock
// backend, to exercise the generic surface-enrollment flow end to end (an
// artifact, prerequisites, config fields, external steps and a verification
// signal). Real surfaces ship their own folder here; the folder name must
// equal the backend descriptor's surface_id.
export default {
  name: 'Example marketplace',
  description:
    'A demonstration surface. It generates a product file for a hypothetical marketplace; once the marketplace fetches it, you are live.',

  // The generic flow previews a surface's checks by id before enrollment.
  check: {
    example_return_policy: 'Return policy confirmed',
    example_feed_generated: 'Product file generated',
    example_products_eligible: 'At least one product eligible',
  },

  prereq: {
    returnPolicy: 'Return policy configured',
    feedGenerated: 'Product file generated',
    productsEligible: 'Products eligible for the marketplace',
  },
  remediation: {
    returnPolicy: 'Set up a return policy',
    feedGenerated: 'We generate this for you — it appears here shortly after you start',
    productsEligible: 'Review your exclusions and notices below',
  },

  config: {
    returnPolicy: {
      label: 'My return policy is configured',
      help: 'The marketplace requires one before it lists your products. Tick this once it is set.',
    },
    excludedSkus: {
      label: 'Products to keep out of the marketplace',
      help: 'One SKU per line. Excluded products stay listed, just not eligible.',
    },
    notices: {
      label: 'Consumer notices',
      help: 'Optional. JSON keyed by SKU, e.g. {"SKU-1": {"required": true, "notice_type": "example", "notice_message": "…"}}. A required notice with no message keeps that product out.',
    },
  },

  artifact: {
    feed: 'Your product file URL',
  },
  artifactDetail: {
    product_count: 'Products in file',
    eligible_count: 'Eligible',
    excluded_count: 'Excluded',
    blocked_needs_notice_count: 'Held out: notice missing its message',
  },

  step: {
    interestForm: 'Submit the marketplace interest form',
    sandboxValidation: 'The marketplace validates your integration in its sandbox',
  },
  signal: {
    example_first_fetch: 'The marketplace to fetch your product file',
  },
}
