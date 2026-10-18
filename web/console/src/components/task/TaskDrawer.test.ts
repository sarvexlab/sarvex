// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// TaskDrawer renders whichever typed payload came back from
// GET /tasks/{id}: a remediations diff with a batch Apply and a Revert once
// applied, a plain field + problem list for issues with no rule-based fix,
// or the consistency finding that raised the task. A task with no payload
// falls back to a line addressed to whoever owns it — and touches no
// remediation endpoint on the way.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { i18n } from '@/i18n'
import TaskDrawer from './TaskDrawer.vue'
import { useStoresStore } from '@/stores/stores'
import { useValidationStore } from '@/stores/validation'
import type { components } from '@/api/schema'

type Task = components['schemas']['task']

vi.mock('@/api/tasks', () => ({
  getTask: vi.fn(),
}))
vi.mock('@/api/remediations', () => ({
  applyRemediations: vi.fn(),
  revertWriteBack: vi.fn(),
}))
vi.mock('@/api/validation', () => ({
  getValidationStatus: vi.fn(async () => ({})),
  listValidationRuns: vi.fn(async () => []),
  triggerValidationRun: vi.fn(),
}))

import { getTask } from '@/api/tasks'
import { applyRemediations, revertWriteBack } from '@/api/remediations'
import { triggerValidationRun } from '@/api/validation'

const baseTask = (over: Partial<Task> = {}): Task => ({
  task_id: 'task-1',
  store_id: 'store-1',
  title: 'Fix product color',
  why_it_matters: 'Blocks checkout for this product.',
  owner: 'merchant',
  resolution: 'one_click',
  state: 'open',
  blocks_checkout: true,
  source: 'readiness',
  source_ref: 'sku-1',
  ...over,
})

async function flushPromises() {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(getTask).mockReset()
  vi.mocked(applyRemediations).mockReset()
  vi.mocked(revertWriteBack).mockReset()
  vi.mocked(triggerValidationRun).mockReset()
})

describe('TaskDrawer', () => {
  it('renders the remediation diff and applies + reverts the batch', async () => {
    const task = baseTask()
    vi.mocked(getTask).mockResolvedValue({
      ...task,
      remediations: [
        { remediation_id: 'r-1', sku: 'sku-1', field: 'color', from_value: 'Rot', to_value: 'Red', type: 'rule', trust: 'high' },
      ],
    })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain('color')
    expect(w.text()).toContain('Rot')
    expect(w.text()).toContain('Red')

    vi.mocked(applyRemediations).mockResolvedValue({ applied: 1, write_back_ids: ['wb-1'] })
    const applyBtn = w.findAll('button').find((b) => b.text().includes('Apply'))
    expect(applyBtn).toBeTruthy()
    await applyBtn!.trigger('click')
    await flushPromises()

    expect(applyRemediations).toHaveBeenCalledWith('store-1', ['r-1'])
    expect(w.text()).toContain('Applied 1 fix')

    vi.mocked(revertWriteBack).mockResolvedValue({ reverted: true })
    const revertBtn = w.findAll('button').find((b) => b.text().includes('Revert'))
    expect(revertBtn).toBeTruthy()
    await revertBtn!.trigger('click')
    await flushPromises()

    expect(revertWriteBack).toHaveBeenCalledWith('store-1', 'wb-1')
    expect(w.text()).toContain('Reverted')
  })

  it('shows a no-fix message for a readiness task with no remediations and no issues, without calling apply', async () => {
    const task = baseTask()
    vi.mocked(getTask).mockResolvedValue({ ...task, remediations: [] })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain('No automatic fix')
    expect(applyRemediations).not.toHaveBeenCalled()
  })

  it('renders the underlying issue detail for a readiness task with no rule-based fix', async () => {
    const task = baseTask()
    vi.mocked(getTask).mockResolvedValue({
      ...task,
      remediations: [],
      issues: [
        { issue_id: 'iss-1', field: 'description', problem: 'free_text', blocks_checkout: true, remediable: 'manual' },
      ],
    })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain('description')
    expect(w.text()).toContain('Free text')
    expect(w.text()).toContain('Blocks')
    expect(w.text()).toContain('update it directly in your store')
    expect(applyRemediations).not.toHaveBeenCalled()
  })

  it('tells a merchant with no payload to fix it in their store', async () => {
    const task = baseTask({ source: 'checklist', source_ref: 'item-1' })
    vi.mocked(getTask).mockResolvedValue({ ...task })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain("you'll need to update it directly in your store")
    expect(getTask).toHaveBeenCalledWith('task-1')
    expect(applyRemediations).not.toHaveBeenCalled()
  })

  // Regression: this task is source=readiness, so branching on source put it
  // in the per-SKU branch and told the merchant to go fix it in their store —
  // directly above a footer saying Sarvex was handling it. Its source_ref is
  // the audit, not a product.
  it('does not tell the merchant to fix the audit milestone themselves', async () => {
    const task = baseTask({
      task_id: 'readiness:store-1:initial_audit',
      title: 'Auditing your catalog',
      source: 'readiness',
      source_ref: 'initial_audit',
      owner: 'sarvex',
      resolution: 'automatic',
      state: 'in_progress',
    })
    vi.mocked(getTask).mockResolvedValue({ ...task })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).not.toContain('update it directly in your store')
    expect(w.text()).toContain("We're on this one")
  })

  // owner=external falls through to the same default message as any other
  // unrecognised owner, rather than a bespoke line.
  it('falls back to the default message for the external owner', async () => {
    const task = baseTask({
      source: 'validation',
      source_ref: 'sandbox',
      owner: 'external',
      resolution: 'external_wait',
      state: 'blocked',
    })
    vi.mocked(getTask).mockResolvedValue({ ...task })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain("you'll need to update it directly in your store")
  })

  it('renders the finding for a monitoring-sourced task', async () => {
    const task = baseTask({
      source: 'monitoring',
      source_ref: 'system:plugin_gateway',
      owner: 'sarvex',
      resolution: 'automatic',
      blocks_checkout: false,
    })
    vi.mocked(getTask).mockResolvedValue({
      ...task,
      finding: {
        finding_id: 'probe:store-1:system:plugin_gateway:profile_format',
        sku: 'system:plugin_gateway',
        field: 'profile_format',
        sources: { error: 'Invalid JSON: unexpected end of JSON input' },
        severity: 'warning',
        detected_at: '2026-07-17T00:00:00Z',
      },
    })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain('profile_format')
    expect(w.text()).toContain('Invalid JSON: unexpected end of JSON input')
    expect(w.text()).toContain('warning')
    expect(w.text()).not.toContain("isn't available yet")
    expect(applyRemediations).not.toHaveBeenCalled()
  })

  it('falls back to the owner line for a monitoring task with no finding payload', async () => {
    const task = baseTask({ source: 'monitoring', source_ref: 'system:plugin_gateway', owner: 'sarvex' })
    vi.mocked(getTask).mockResolvedValue({ ...task })

    const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task } })
    await flushPromises()

    expect(w.text()).toContain("We're on this one")
  })

  // Regression: this is the actual bug report — the "Run pre-validation"
  // task is owner=sarvex, so it fell through to the generic "Sarvex is
  // handling this — nothing for you to do" line even though the merchant
  // can and should be able to trigger and see pre-validation results here,
  // the same as at /validation.
  describe('pre-validation milestone/failure tasks', () => {
    const preValidationTask = baseTask({
      title: 'Run pre-validation',
      why_it_matters: "Every store must pass Sarvex's internal pre-validation suite before attempting Google's sandbox.",
      owner: 'sarvex',
      resolution: 'one_click',
      state: 'open',
      source: 'validation',
      source_ref: 'run',
    })

    it('offers the trigger button instead of the generic sarvex line, and calls the API on click', async () => {
      useStoresStore().stores = [{ store_id: 'store-1', name: 'Acme', platform: 'woocommerce' }]
      useStoresStore().currentStoreId = 'store-1'
      const validation = useValidationStore()
      validation.status = { pre_validation: 'not_run' }
      validation.loaded = true
      vi.mocked(getTask).mockResolvedValue({ ...preValidationTask })
      vi.mocked(triggerValidationRun).mockResolvedValue({ job_id: 'j1', status: 'queued' })

      const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task: preValidationTask } })
      await flushPromises()

      expect(w.text()).not.toContain('Sarvex is handling this')
      const btn = w.findAll('button').find((b) => b.text().includes('Run pre-validation'))
      expect(btn).toBeTruthy()

      await btn!.trigger('click')
      await flushPromises()

      expect(triggerValidationRun).toHaveBeenCalledWith('store-1')
    })

    it('shows open failures and run history from the validation store', async () => {
      const validation = useValidationStore()
      validation.status = {
        pre_validation: 'failing',
        open_failures: [{ requirement: 'profile_has_signing_keys', summary: 'profile missing signing_keys' }],
      }
      validation.runs = [
        { run_id: 'run-1', ran_at: '2026-08-02T00:00:00Z', spec_version: '1.0', result: 'fail', passed: 2, failed: 1, warnings: 0 },
      ]
      validation.loaded = true
      vi.mocked(getTask).mockResolvedValue({ ...preValidationTask })

      const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task: preValidationTask } })
      await flushPromises()

      expect(w.text()).toContain('profile_has_signing_keys')
      expect(w.text()).toContain('run-1')
      expect(w.text()).toContain('2 pass')
      expect(w.text()).toContain('1 fail')
    })

    it('closes rather than offering a resolve action, since the task retires itself server-side', async () => {
      const validation = useValidationStore()
      validation.status = { pre_validation: 'not_run' }
      validation.loaded = true
      vi.mocked(getTask).mockResolvedValue({ ...preValidationTask })

      const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task: preValidationTask } })
      await flushPromises()

      const closeBtn = w.findAll('button').find((b) => b.text().trim() === 'Close')
      expect(closeBtn).toBeTruthy()
      await closeBtn!.trigger('click')
      expect(w.emitted('close')).toBeTruthy()
    })

    it('renders the failure task through the same panel, not the merchant resolve button', async () => {
      const validation = useValidationStore()
      validation.status = { pre_validation: 'failing' }
      validation.loaded = true
      const failureTask = baseTask({
        title: 'Pre-validation checks failed',
        owner: 'merchant',
        resolution: 'guided',
        state: 'open',
        source: 'validation',
        source_ref: 'pre_validation_failed',
      })
      vi.mocked(getTask).mockResolvedValue({ ...failureTask })

      const w = mount(TaskDrawer, { global: { plugins: [i18n] }, props: { task: failureTask } })
      await flushPromises()

      const btn = w.findAll('button').find((b) => b.text().includes('Run pre-validation'))
      expect(btn).toBeTruthy()
      // Regression: this task is owner=merchant/resolution=guided, which
      // would otherwise match the drawer's generic merchant-resolve footer
      // button — clicking "Mark as done" without a real passing run would
      // close the task while pre-validation is still actually failing.
      expect(w.findAll('button').some((b) => b.text().includes('Mark as done'))).toBe(false)
      expect(w.findAll('button').some((b) => b.text().trim() === 'Close')).toBe(true)
    })
  })
})
