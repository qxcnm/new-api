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

For commercial licensing, please contact support@quantumnous.com
*/
import { expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const form = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Shared provider',
  key: 'test-key',
  models: 'gpt-4o,claude-3',
  group: ['default', 'vip'],
}

test.each([
  ['', true],
  ['{}', true],
  ['{"gpt-4o":["vip"]}', true],
  ['{"gpt-4o":["default","vip"],"claude-3":["vip"]}', true],
  ['{"gpt-4o":[]}', false],
  ['{"missing-model":["vip"]}', false],
  ['{"gpt-4o":["unknown-group"]}', false],
  ['{"gpt-4o":["vip","vip"]}', false],
  ['{"gpt-4o":[" vip"]}', false],
  ['{" gpt-4o":["vip"]}', false],
  ['{"gpt-4o":[""]}', false],
  ['{"gpt-4o":"vip"}', false],
  ['{"gpt-4o":[1]}', false],
  ['{"gpt-4o":', false],
  ['null', false],
  ['[]', false],
])(
  'model bindings %s have expected form validity %s',
  (model_groups, valid) => {
    const result = channelFormSchema.safeParse({ ...form, model_groups })
    expect(result.success).toBe(valid)
    if (!result.success) {
      expect(result.error.issues[0].path).toEqual(['model_groups'])
    }
  }
)

test('created bindings survive reading a channel and updating its form', () => {
  const model_groups = '{"gpt-4o":["default"],"claude-3":["vip"]}'
  const created = transformFormDataToCreatePayload({ ...form, model_groups })
  expect(created.channel.model_groups).toBe(model_groups)
  const saved = channelSchema.parse({
    ...created.channel,
    id: 12,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    other: '',
    remark: '',
  })
  const defaults = transformChannelToFormDefaults(saved)
  expect(defaults.model_groups).toBe(model_groups)
  expect(
    transformFormDataToUpdatePayload(defaults, saved.id).model_groups
  ).toBe(model_groups)
})

test('clearing bindings sends an explicit empty update to restore channel groups', () => {
  const payload = transformFormDataToUpdatePayload(
    { ...form, model_groups: '' },
    12
  )
  expect(payload.model_groups).toBe('')
})

test('removing a channel group requires updating affected model bindings', () => {
  const result = channelFormSchema.safeParse({
    ...form,
    group: ['default'],
    model_groups: '{"gpt-4o":["vip"]}',
  })
  expect(result.success).toBe(false)
  if (!result.success) {
    expect(result.error.issues[0].message).toBe(
      'Model group bindings must use groups selected for this channel'
    )
  }
})

test.each([
  ['A'.repeat(64), true],
  ['A'.repeat(65), false],
  ['组'.repeat(21), true],
  ['组'.repeat(22), false],
  ['default,vip', false],
  [' vip', false],
])('group %s respects the server group-name limits', (group, valid) => {
  const result = channelFormSchema.safeParse({
    ...form,
    group: [group],
    model_groups: JSON.stringify({ 'gpt-4o': [group] }),
  })
  expect(result.success).toBe(valid)
})
