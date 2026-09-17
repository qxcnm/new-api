import { describe, expect, it } from 'vitest'

import { buildQueryParams } from '../filters'

describe('dashboard filter query params', () => {
  it('includes the selected model name', () => {
    expect(
      buildQueryParams(
        { start_timestamp: 100, end_timestamp: 200 },
        { time_granularity: 'day', model_name: 'gpt-4.1' }
      )
    ).toMatchObject({
      start_timestamp: 100,
      end_timestamp: 200,
      default_time: 'day',
      model_name: 'gpt-4.1',
    })
  })

  it('omits an empty model name', () => {
    expect(
      buildQueryParams(
        { start_timestamp: 100, end_timestamp: 200 },
        { time_granularity: 'day', model_name: '' }
      )
    ).not.toHaveProperty('model_name')
  })
})
