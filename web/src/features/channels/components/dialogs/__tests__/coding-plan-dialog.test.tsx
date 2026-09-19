/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import {
  getCodingPlanQuota,
  getGLMResetCards,
  resetGLMCard,
} from '../../../api'
import { channelSchema } from '../../../types'
import { useChannels } from '../../channels-provider'
import { CodingPlanDialog } from '../coding-plan-dialog'

vi.mock('../../../api', () => ({
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
  vi.mocked(getGLMResetCards).mockReset()
  vi.mocked(resetGLMCard).mockReset()
})

afterEach(() => {
  cleanup()
  client.clear()
})

function renderDialog(mode: 'quota' | 'reset-cards') {
  return render(
    <QueryClientProvider client={client}>
      <CodingPlanDialog mode={mode} open onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
}

describe('CodingPlan dialog', () => {
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
