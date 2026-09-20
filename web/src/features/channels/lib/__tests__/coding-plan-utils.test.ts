/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import { getCodingPlanAccessMode, isCodingPlanChannel } from '../channel-utils'

describe('CodingPlan channel actions', () => {
  test.each([
    [' glm-coding-plan-international/ ', 'glm-coding-plan-international'],
    ['kimi-coding-plan', 'kimi-coding-plan'],
    ['minimax-coding-plan', 'minimax-coding-plan'],
    ['minimax-coding-plan-international', 'minimax-coding-plan-international'],
    ['https://api.example.com', 'standard'],
  ])('recognizes the saved access alias %s', (baseUrl, expected) => {
    expect(getCodingPlanAccessMode(baseUrl)).toBe(expected)
  })

  test('shows plan actions only for server-identified plans', () => {
    expect(
      isCodingPlanChannel({
        channel_info: { is_plan: true, plan_name: 'glm-coding-plan' },
      } as Channel)
    ).toBe(true)
    expect(
      isCodingPlanChannel({
        channel_info: { is_plan: false, plan_name: 'glm-coding-plan' },
      } as Channel)
    ).toBe(false)
    expect(isCodingPlanChannel({} as Channel)).toBe(false)
  })
})
