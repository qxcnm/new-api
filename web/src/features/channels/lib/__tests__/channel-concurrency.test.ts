/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  MAX_CHANNEL_CONCURRENCY,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const validForm = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Concurrency channel',
  key: 'test-key',
  models: 'gpt-4o',
}

test.each([
  [0, true],
  [1, true],
  [MAX_CHANNEL_CONCURRENCY, true],
  [-1, false],
  [MAX_CHANNEL_CONCURRENCY + 1, false],
  [1.5, false],
])(
  'channel concurrency value %s has expected form validity %s',
  (max_concurrency, valid) => {
    const result = channelFormSchema.safeParse({
      ...validForm,
      max_concurrency,
    })
    expect(result.success).toBe(valid)
    if (!valid && !result.success) {
      expect(result.error.issues[0]?.path).toEqual(['max_concurrency'])
    }
  }
)

test('channel concurrency is read from and written to channel_info', () => {
  const saved = channelSchema.parse({
    ...transformFormDataToCreatePayload({
      ...validForm,
      max_concurrency: 7,
    }).channel,
    id: 12,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    other: '',
    balance_updated_time: 0,
    remark: '',
  })

  const defaults = transformChannelToFormDefaults(saved)
  expect(defaults.max_concurrency).toBe(7)
  expect(
    transformFormDataToUpdatePayload(defaults, saved.id).channel_info
  ).toEqual({ max_concurrency: 7 })
})

test('zero concurrency keeps the channel unlimited when updating', () => {
  const payload = transformFormDataToUpdatePayload(
    { ...validForm, max_concurrency: 0 },
    12
  )
  expect(payload.channel_info).toEqual({ max_concurrency: 0 })
})
