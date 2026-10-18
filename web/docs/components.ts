// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineComponents } from "blume";

// Layout slots that carry the Sarvex design language into the docs chrome:
// the marketing site's mark in the header, and its footer below the content.
// Everything else is restyled through theme.css.
export default defineComponents({
  layout: {
    Logo: "./components/Logo.astro",
    Footer: "./components/Footer.astro",
  },
});
