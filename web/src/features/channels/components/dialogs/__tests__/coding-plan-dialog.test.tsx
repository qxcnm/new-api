/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import {
  getCodingPlanKeys,
  getCodingPlanQuota,
  getGLMResetCards,
  getGLMRiskStatus,
  resetGLMCard,
} from '../../../api'
import {
  channelSchema,
  type Channel,
  type CodingPlanQuotaResponse,
} from '../../../types'
import { useChannels } from '../../channels-provider'
import { CodingPlanDialog } from '../coding-plan-dialog'

vi.mock('../../../api', () => ({
  getCodingPlanKeys: vi.fn(),
  getCodingPlanQuota: vi.fn(),
  getGLMResetCards: vi.fn(),
  getGLMRiskStatus: vi.fn(),
  resetGLMCard: vi.fn(),
}))
vi.mock('../../channels-provider', () => ({ useChannels: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const row = channelSchema.parse({
  id: 7,
  name: 'GLM plan',
  type: 26,
  key: '',
  status: 1,
  created_time: 1,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
  models: 'glm-4.5',
  group: 'default',
  channel_info: { is_plan: true, plan_name: 'glm-coding-plan' },
})

let client: QueryClient

beforeEach(async () => {
  await i18next.changeLanguage('en')
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.mocked(useChannels).mockReturnValue({
    currentRow: row,
  } as ReturnType<typeof useChannels>)
  vi.mocked(getCodingPlanQuota).mockReset()
  vi.mocked(getCodingPlanKeys).mockReset()
  vi.mocked(getGLMRiskStatus).mockReset()
  vi.mocked(getGLMResetCards).mockReset()
  vi.mocked(resetGLMCard).mockReset()
})

afterEach(() => {
  cleanup()
  client.clear()
})

function renderDialog(mode: 'quota' | 'risk' | 'reset-cards') {
  return render(
    <QueryClientProvider client={client}>
      <CodingPlanDialog mode={mode} open onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
}

function useMultiKeyGLM(planName = 'glm-coding-plan') {
  vi.mocked(useChannels).mockReturnValue({
    currentRow: {
      ...row,
      key: 'full-secret-abcd\nfull-secret-efgh\nfull-secret-ijkl',
      channel_info: {
        ...row.channel_info,
        plan_name: planName,
        is_multi_key: true,
      },
    },
  } as ReturnType<typeof useChannels>)
  vi.mocked(getCodingPlanKeys).mockResolvedValue({
    success: true,
    data: {
      keys: [
        { index: 0, identifier: '****abcd', enabled: false },
        { index: 1, identifier: '****efgh', enabled: true },
        { index: 2, identifier: '****ijkl', enabled: true },
      ],
    },
  })
}

const quotaData: NonNullable<CodingPlanQuotaResponse['data']> = {
  plan_name: 'glm-coding-plan',
  quota_supported: true,
  tiers: [{ name: 'five_hour', remaining: 80, limit: 100, used: 20 }],
}

const quotaResponse: CodingPlanQuotaResponse = {
  success: true,
  data: quotaData,
}

function pendingResponse<T>() {
  let resolve!: (response: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('CodingPlan dialog', () => {
  test('multi-key GLM selects the first enabled key and only queries that key', async () => {
    useMultiKeyGLM()
    vi.mocked(getCodingPlanQuota).mockResolvedValue(quotaResponse)
    renderDialog('quota')
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Query key' })
      ).toHaveTextContent('Key 2 · ****efgh')
    )
    await waitFor(() =>
      expect(getCodingPlanQuota).toHaveBeenCalledExactlyOnceWith(7, 1)
    )
    await userEvent.click(screen.getByRole('combobox', { name: 'Query key' }))
    const disabledKey = await screen.findByRole('option', {
      name: /Key 1 · \*\*\*\*abcd\s*\(Disabled\)/,
    })
    expect(disabledKey).toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByRole('option', { name: 'Key 2 · ****efgh' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(document.body).not.toHaveTextContent('full-secret-')
  })

  test.each(['glm-coding-plan', 'glm-coding-plan-international'])(
    '%s switching keys queries risk for that index and refresh uses the selection',
    async (planName) => {
      useMultiKeyGLM(planName)
      vi.mocked(getGLMRiskStatus)
        .mockResolvedValueOnce({
          success: true,
          data: { plan_name: planName, status: 'risk' },
        })
        .mockResolvedValue({
          success: true,
          data: { plan_name: planName, status: 'normal' },
        })
      renderDialog('risk')
      expect(await screen.findByText('Risk Control')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('combobox', { name: 'Query key' }))
      await userEvent.click(
        await screen.findByRole('option', { name: 'Key 3 · ****ijkl' })
      )
      expect(await screen.findByText('Normal')).toBeInTheDocument()
      expect(getGLMRiskStatus).toHaveBeenNthCalledWith(1, 7, 1)
      expect(getGLMRiskStatus).toHaveBeenNthCalledWith(2, 7, 2)
      await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await waitFor(() =>
        expect(getGLMRiskStatus).toHaveBeenNthCalledWith(3, 7, 2)
      )
      expect(getCodingPlanKeys).toHaveBeenCalledTimes(2)
    }
  )

  test('changing keys while a request is pending clears old data and ignores its later result', async () => {
    useMultiKeyGLM()
    const pending = pendingResponse<CodingPlanQuotaResponse>()
    vi.mocked(getCodingPlanQuota)
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue({
        success: true,
        data: {
          ...quotaData,
          tiers: [{ name: 'five_hour', remaining: 25, limit: 100, used: 75 }],
        },
      })
    renderDialog('quota')
    await waitFor(() => expect(getCodingPlanQuota).toHaveBeenCalledWith(7, 1))
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
    await userEvent.click(screen.getByRole('combobox', { name: 'Query key' }))
    await userEvent.click(
      await screen.findByRole('option', { name: 'Key 3 · ****ijkl' })
    )
    expect(await screen.findByText('Remaining: 25 / 100')).toBeInTheDocument()
    await act(async () => pending.resolve(quotaResponse))
    expect(screen.queryByText('Remaining: 80 / 100')).not.toBeInTheDocument()
    expect(screen.getByText('Remaining: 25 / 100')).toBeInTheDocument()
  })

  test('metadata loading and failures prevent upstream queries and refresh retries discovery', async () => {
    useMultiKeyGLM()
    const pending =
      pendingResponse<Awaited<ReturnType<typeof getCodingPlanKeys>>>()
    vi.mocked(getCodingPlanKeys).mockReturnValueOnce(pending.promise)
    vi.mocked(getCodingPlanQuota).mockResolvedValue(quotaResponse)
    renderDialog('quota')
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Query key' })).toBeDisabled()
    expect(getCodingPlanQuota).not.toHaveBeenCalled()
    await act(async () =>
      pending.reject(new Error('Failed to load channel keys'))
    )
    expect(
      await screen.findByText('Failed to load channel keys')
    ).toBeInTheDocument()
    expect(getCodingPlanQuota).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
  })

  test.each([
    { keys: [] },
    { keys: [{ index: 0, identifier: '****', enabled: false }] },
  ])(
    'a key list without an enabled key shows an empty state and makes no upstream request',
    async ({ keys }) => {
      useMultiKeyGLM()
      vi.mocked(getCodingPlanKeys).mockResolvedValue({
        success: true,
        data: { keys },
      })
      renderDialog('risk')
      expect(
        await screen.findByText('No enabled keys available')
      ).toBeInTheDocument()
      expect(getGLMRiskStatus).not.toHaveBeenCalled()
      await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await waitFor(() => expect(getCodingPlanKeys).toHaveBeenCalledTimes(2))
      expect(getGLMRiskStatus).not.toHaveBeenCalled()
    }
  )

  test('a rejected selected key shows an inline error and another key can still be queried', async () => {
    useMultiKeyGLM()
    vi.mocked(getCodingPlanQuota)
      .mockResolvedValueOnce({
        success: false,
        message: 'The selected CodingPlan key is disabled',
      })
      .mockResolvedValue(quotaResponse)
    renderDialog('quota')
    expect(
      await screen.findByText('The selected CodingPlan key is disabled')
    ).toBeInTheDocument()
    await userEvent.click(screen.getByRole('combobox', { name: 'Query key' }))
    await userEvent.click(
      await screen.findByRole('option', { name: 'Key 3 · ****ijkl' })
    )
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
    expect(getCodingPlanQuota).toHaveBeenLastCalledWith(7, 2)
    expect(document.body).not.toHaveTextContent('full-secret-')
  })

  test('refresh updates key availability without silently selecting a different key', async () => {
    useMultiKeyGLM()
    vi.mocked(getCodingPlanQuota).mockResolvedValue(quotaResponse)
    renderDialog('quota')
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
    vi.mocked(getCodingPlanKeys).mockResolvedValue({
      success: true,
      data: {
        keys: [
          { index: 1, identifier: '****efgh', enabled: false },
          { index: 2, identifier: '****ijkl', enabled: true },
        ],
      },
    })
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(
      await screen.findByText('The selected CodingPlan key is disabled')
    ).toBeInTheDocument()
    expect(getCodingPlanQuota).toHaveBeenCalledExactlyOnceWith(7, 1)
    const selector = screen.getByRole('combobox', { name: 'Query key' })
    selector.focus()
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{Enter}')
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
    expect(getCodingPlanQuota).toHaveBeenLastCalledWith(7, 2)
  })

  test('a multi-key quota response without tiers shows the shared empty state', async () => {
    useMultiKeyGLM()
    vi.mocked(getCodingPlanQuota).mockResolvedValue({
      success: true,
      data: { ...quotaData, tiers: [] },
    })
    renderDialog('quota')
    expect(await screen.findByText('No Data')).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Query key' })
    ).toHaveTextContent('Key 2 · ****efgh')
  })

  test('Chinese key labels and selection errors use the same locale as the dialog', async () => {
    useMultiKeyGLM()
    i18next.addResourceBundle('zh', 'translation', zh.translation)
    await i18next.changeLanguage('zh')
    vi.mocked(getCodingPlanQuota).mockResolvedValue({
      success: false,
      message: 'CodingPlan key selection is invalid',
    })
    renderDialog('quota')
    expect(
      await screen.findByText(
        zh.translation['CodingPlan key selection is invalid']
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: zh.translation['Query key'] })
    ).toHaveTextContent('密钥 2 · ****efgh')
  })

  test('multi-key reset cards retain their existing request and never show a key selector', async () => {
    useMultiKeyGLM()
    vi.mocked(getGLMResetCards).mockResolvedValue({
      success: false,
      message: 'CodingPlan queries do not support multi-key channels',
    })
    renderDialog('reset-cards')
    expect(
      await screen.findByText(
        'CodingPlan queries do not support multi-key channels'
      )
    ).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(getCodingPlanKeys).not.toHaveBeenCalled()
    expect(getGLMResetCards).toHaveBeenCalledExactlyOnceWith(7)
  })

  test.each([
    ['glm-coding-plan', false, true],
    ['kimi-coding-plan', true, true],
    ['minimax-coding-plan', true, true],
    ['glm-coding-plan', true, false],
  ])(
    '%s with multi-key=%s and plan=%s keeps the original request and has no selector',
    async (planName, multiKey, isPlan) => {
      vi.mocked(useChannels).mockReturnValue({
        currentRow: {
          ...row,
          channel_info: {
            ...row.channel_info,
            plan_name: planName,
            is_plan: isPlan,
            is_multi_key: multiKey,
          },
        },
      } as ReturnType<typeof useChannels>)
      vi.mocked(getCodingPlanQuota).mockResolvedValue(quotaResponse)
      renderDialog('quota')
      expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
      expect(getCodingPlanKeys).not.toHaveBeenCalled()
      expect(getCodingPlanQuota).toHaveBeenCalledExactlyOnceWith(7)
    }
  )

  test('reopening or changing the channel reloads metadata and ignores earlier responses', async () => {
    useMultiKeyGLM()
    const firstKeys =
      pendingResponse<Awaited<ReturnType<typeof getCodingPlanKeys>>>()
    vi.mocked(getCodingPlanKeys).mockReturnValueOnce(firstKeys.promise)
    vi.mocked(getCodingPlanQuota).mockResolvedValue(quotaResponse)
    const view = renderDialog('quota')
    view.rerender(
      <QueryClientProvider client={client}>
        <CodingPlanDialog mode='quota' open={false} onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )
    const currentRow = vi.mocked(useChannels).getMockImplementation()?.()
      .currentRow as Channel
    vi.mocked(useChannels).mockReturnValue({
      currentRow: { ...currentRow, id: 8 },
    } as ReturnType<typeof useChannels>)
    view.rerender(
      <QueryClientProvider client={client}>
        <CodingPlanDialog mode='quota' open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
    expect(getCodingPlanKeys).toHaveBeenNthCalledWith(2, 8)
    expect(getCodingPlanQuota).toHaveBeenCalledExactlyOnceWith(8, 1)
    await act(async () =>
      firstKeys.resolve({
        success: true,
        data: { keys: [{ index: 9, identifier: '****late', enabled: true }] },
      })
    )
    expect(
      screen.getByRole('combobox', { name: 'Query key' })
    ).toHaveTextContent('Key 2 · ****efgh')
    expect(getCodingPlanQuota).toHaveBeenCalledTimes(1)
  })

  test.each(['glm-coding-plan', 'kimi-coding-plan', 'minimax-coding-plan'])(
    '%s displays reset timestamps in local time and labels quota periods',
    async (planName) => {
      // Fixed local wall time, serialized as an upstream UTC timestamp.
      const resetsAt = new Date(2026, 8, 20, 5, 30, 28).toISOString()
      vi.mocked(getCodingPlanQuota).mockResolvedValue({
        success: true,
        data: {
          plan_name: planName,
          quota_supported: true,
          tiers: [
            {
              name: 'five_hour',
              remaining: 80,
              limit: 100,
              used: 20,
              resets_at: resetsAt,
            },
            { name: 'weekly_limit', remaining: 800, limit: 1000, used: 200 },
          ],
        },
      })
      renderDialog('quota')
      expect(
        await screen.findByText('Resets at: 2026-09-20 05:30:28 (local time)')
      ).toBeInTheDocument()
      expect(screen.getByText('Five-hour quota')).toBeInTheDocument()
      expect(screen.getByText('Weekly quota')).toBeInTheDocument()
      expect(screen.queryByText('five_hour')).not.toBeInTheDocument()
      expect(screen.queryByText('weekly_limit')).not.toBeInTheDocument()
      expect(screen.getAllByText(/^Resets at:/)).toHaveLength(1)
    }
  )

  test('Chinese quota labels and local-time descriptions use locale translations', async () => {
    i18next.addResourceBundle('zh', 'translation', zh.translation)
    await i18next.changeLanguage('zh')
    vi.mocked(getCodingPlanQuota).mockResolvedValue({
      success: true,
      data: {
        plan_name: 'glm-coding-plan',
        quota_supported: true,
        tiers: [
          {
            name: 'five_hour',
            remaining: 80,
            limit: 100,
            used: 20,
            resets_at: new Date(2026, 8, 20, 5, 30, 28).toISOString(),
          },
          { name: 'weekly_limit', remaining: 800, limit: 1000, used: 200 },
        ],
      },
    })
    renderDialog('quota')
    expect(
      await screen.findByText('重置时间：2026-09-20 05:30:28（本地时间）')
    ).toBeInTheDocument()
    expect(screen.getByText('五小时额度')).toBeInTheDocument()
    expect(screen.getByText('每周额度')).toBeInTheDocument()
  })

  test('invalid reset timestamps show an unknown time instead of raw text', async () => {
    vi.mocked(getCodingPlanQuota).mockResolvedValue({
      success: true,
      data: {
        plan_name: 'glm-coding-plan',
        quota_supported: true,
        tiers: [
          {
            name: 'five_hour',
            remaining: 80,
            limit: 100,
            used: 20,
            resets_at: 'malformed-upstream-time',
          },
        ],
      },
    })
    renderDialog('quota')
    expect(
      await screen.findByText('Resets at: Unknown (local time)')
    ).toBeInTheDocument()
    expect(
      screen.queryByText(/malformed-upstream-time/)
    ).not.toBeInTheDocument()
  })

  test('reset card expiry dates use the same local-time display', async () => {
    vi.mocked(getGLMResetCards).mockResolvedValue({
      success: true,
      data: {
        plan_name: 'glm-coding-plan',
        five_hour_resets: [
          {
            recordId: 12,
            expireTime: new Date(2026, 9, 1, 0, 0, 0).toISOString(),
            available: true,
          },
        ],
        week_resets: [],
      },
    })
    renderDialog('reset-cards')
    expect(
      await screen.findByText('Expires: 2026-10-01 00:00:00 (local time)')
    ).toBeInTheDocument()
  })

  test('shows loading and then quota data with refresh support', async () => {
    let resolveQuota!: (value: unknown) => void
    vi.mocked(getCodingPlanQuota).mockReturnValue(
      new Promise((resolve) => {
        resolveQuota = resolve
      }) as never
    )
    renderDialog('quota')
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    resolveQuota({
      success: true,
      data: {
        plan_name: 'glm-coding-plan',
        quota_supported: true,
        tiers: [{ name: 'five_hour', remaining: 80, limit: 100, used: 20 }],
      },
    })
    expect(await screen.findByText('Remaining: 80 / 100')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(getCodingPlanQuota).toHaveBeenCalledTimes(2))
  })

  test('shows an inline error when quota loading fails', async () => {
    vi.mocked(getCodingPlanQuota).mockResolvedValue({
      success: false,
      message: 'upstream unavailable',
    })
    renderDialog('quota')
    expect(await screen.findByText('Request failed')).toBeInTheDocument()
    expect(screen.getByText('upstream unavailable')).toBeInTheDocument()
  })

  test('requires a second confirmation before using a reset card', async () => {
    vi.mocked(getGLMResetCards).mockResolvedValue({
      success: true,
      data: {
        plan_name: 'glm-coding-plan',
        five_hour_resets: [
          { recordId: 12, expireTime: 'tomorrow', available: true },
        ],
        week_resets: [],
      },
    })
    vi.mocked(resetGLMCard).mockResolvedValue({
      success: true,
      data: { record_id: 12 },
    })
    renderDialog('reset-cards')
    const useButtons = await screen.findAllByRole('button', {
      name: 'Use Reset Card',
    })
    fireEvent.click(useButtons[0])
    expect(screen.getByText(/Use reset card #12 now/)).toBeInTheDocument()
    const confirmButtons = screen.getAllByRole('button', {
      name: 'Use Reset Card',
    })
    const confirmButton = confirmButtons.at(-1)
    expect(confirmButton).toBeDefined()
    if (confirmButton) fireEvent.click(confirmButton)
    await waitFor(() =>
      expect(resetGLMCard).toHaveBeenCalledWith(7, {
        record_id: 12,
        reset_type: 'FIVE_HOUR',
      })
    )
    await waitFor(() => expect(getGLMResetCards).toHaveBeenCalledTimes(2))
  })
})
