// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from "blume";

export default defineConfig({
  // Every page's <title> is templated as "{page title} · {this}" by
  // Blume itself — the "Sarvex Docs - Sarvex Docs" duplication on the docs
  // home page came from its own frontmatter `title` also being "Sarvex
  // Docs" (see content/index.mdx, now "Overview"), not from this value.
  title: "Sarvex Docs",
  description:
    "Guides, API reference and developer documentation for integrating with Sarvex and going live on agent checkout.",
  content: {
    root: "content",
  },
  // Built and viewed locally (`make docs`); not deployed — see ./README.md.
  logo: {
    href: "/",
  },
  navigation: {
    cta: { label: "Register interest", href: "https://sarvex.io/contact" },
    tabs: [
      { label: "Guides", path: "/" },
      { label: "API", path: "/api" },
      { label: "Control-plane API", path: "/api/control-plane" },
      { label: "Events", path: "/api/events" },
      { label: "Developers", path: "/developers" },
      { label: "Contributing", path: "/contributing" },
    ],
  },
  // The API reference is generated from the repository's contracts, which
  // stay the single source of truth (sarvex-v1-architecture.md) — never copied here.
  openapi: {
    enabled: true,
    route: "/api/control-plane",
    spec: "../../sarvex-v1-control-plane.yaml",
  },
  asyncapi: {
    enabled: true,
    route: "/api/events",
    spec: "../../sarvex-v1-syncing-plane-async.yaml",
  },
  theme: {
    // Mirrors the marketing site's tokens (assets/css/tokens.css in the site
    // root). Blume sets its own text-on-accent colour, so the dark accent is
    // paired with a dark --blume-accent-foreground in theme.css to stay
    // legible, same as the site's --accent-contrast.
    accent: { dark: "#9d9bff", light: "#4f46e5" },
    background: { dark: "#07070b", light: "#fbfbf8" },
    radius: "lg",
    // Self-hosted from ./fonts — the same woff2 files the marketing site
    // ships — so the docs build never depends on reaching Google Fonts.
    fonts: {
      display: {
        name: "Geist",
        variants: [
          { src: "./fonts/Geist-400.woff2", weight: 400 },
          { src: "./fonts/Geist-500.woff2", weight: 500 },
          { src: "./fonts/Geist-600.woff2", weight: 600 },
        ],
      },
      body: {
        name: "Geist",
        variants: [
          { src: "./fonts/Geist-400.woff2", weight: 400 },
          { src: "./fonts/Geist-500.woff2", weight: 500 },
          { src: "./fonts/Geist-600.woff2", weight: 600 },
        ],
      },
      mono: {
        name: "Geist Mono",
        variants: [{ src: "./fonts/Geist-Mono-400.woff2", weight: 400 }],
      },
    },
  },
  deployment: {
    site: "https://sarvex.io", // static is the default output
  },
});
