/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { getCodingPlanKeys, getCodingPlanQuota, getGLMRiskStatus } from '../api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

afterEach(() => vi.resetAllMocks())

describe('CodingPlan query API', () => {
  test.each([
    [getCodingPlanQuota, '/api/channel/plan/quota/7'],
    [getGLMRiskStatus, '/api/channel/plan/glm/risk/7'],
  ] as const)(
    '%s sends only the selected zero-based index and omits it for single-key requests',
    async (query, path) => {
      vi.mocked(api.get).mockResolvedValue({ data: { success: true } })
      await query(7, 0)
      expect(api.get).toHaveBeenLastCalledWith(path, {
        disableDuplicate: true,
        skipBusinessError: true,
        skipErrorHandler: true,
        params: { key_index: 0 },
      })
      await query(7, 2)
      expect(api.get).toHaveBeenLastCalledWith(
        path,
        expect.objectContaining({ params: { key_index: 2 } })
      )
      await query(7)
      expect(api.get).toHaveBeenLastCalledWith(path, {
        disableDuplicate: true,
        skipBusinessError: true,
        skipErrorHandler: true,
      })
    }
  )

  test('key discovery uses the read-only metadata endpoint', async () => {
    const response = {
      success: true,
      data: { keys: [{ index: 0, identifier: '****abcd', enabled: true }] },
    }
    vi.mocked(api.get).mockResolvedValue({ data: response })
    expect(await getCodingPlanKeys(7)).toEqual(response)
    expect(api.get).toHaveBeenCalledExactlyOnceWith(
      '/api/channel/plan/keys/7',
      {
        disableDuplicate: true,
        skipBusinessError: true,
        skipErrorHandler: true,
      }
    )
  })
})
