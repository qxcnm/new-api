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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../../lib/channel-form'
import { channelSchema } from '../../types'
import { ModelSchedulesEditor } from '../model-schedules-editor'

const form = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Shared provider',
  key: 'test-key',
  models: 'gpt-4o,claude-3',
  group: ['default'],
}
const weekdayWindow = { weekday_mask: 62, start_minute: 540, end_minute: 1080 }

function Fixture(props: { value?: string; disabled?: boolean }) {
  const [value, setValue] = useState(props.value ?? '')
  return (
    <>
      <ModelSchedulesEditor
        value={value}
        onChange={setValue}
        models={['gpt-4o', 'claude-3']}
        disabled={props.disabled}
      />
      <output aria-label='Saved schedules'>{value}</output>
    </>
  )
}

test('model windows support keyboard weekday changes and exact minute editing independently', async () => {
  const user = userEvent.setup()
  render(<Fixture />)
  await user.click(
    screen.getByRole('button', { name: 'Add time window for gpt-4o' })
  )
  const window = screen.getByRole('group', { name: 'Time window 1 for gpt-4o' })
  await user.click(within(window).getByRole('button', { name: 'Weekdays' }))
  const monday = within(window).getByRole('button', { name: 'Monday' })
  monday.focus()
  await user.keyboard(' ')
  expect(monday).toHaveAttribute('aria-pressed', 'false')
  await user.selectOptions(
    within(window).getByRole('combobox', { name: 'Start minute' }),
    '15'
  )
  await user.selectOptions(
    within(window).getByRole('combobox', { name: 'End hour' }),
    '24'
  )
  expect(
    within(window).getByRole('combobox', { name: 'End minute' })
  ).toBeDisabled()
  expect(
    JSON.parse(screen.getByLabelText('Saved schedules').textContent ?? '')
  ).toEqual({
    'gpt-4o': [{ weekday_mask: 60, start_minute: 555, end_minute: 1440 }],
  })
  expect(
    screen.getByRole('button', { name: 'Add time window for claude-3' })
  ).toBeEnabled()
  await user.click(
    screen.getByRole('button', { name: 'Add time window for claude-3' })
  )
  await user.click(within(window).getByRole('button', { name: 'Remove' }))
  expect(
    JSON.parse(screen.getByLabelText('Saved schedules').textContent ?? '')
  ).toEqual({
    'claude-3': [{ weekday_mask: 127, start_minute: 540, end_minute: 1080 }],
  })
})

test('overlapping windows show an error and become valid after moving the second to an adjacent interval', async () => {
  const user = userEvent.setup()
  render(<Fixture value={JSON.stringify({ 'gpt-4o': [weekdayWindow] })} />)
  await user.click(
    screen.getByRole('button', { name: 'Add time window for gpt-4o' })
  )
  expect(screen.getByRole('alert')).toHaveTextContent('cannot overlap')
  const window = screen.getByRole('group', { name: 'Time window 2 for gpt-4o' })
  await user.selectOptions(
    within(window).getByRole('combobox', { name: 'End hour' }),
    '24'
  )
  await user.selectOptions(
    within(window).getByRole('combobox', { name: 'Start hour' }),
    '18'
  )
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('JSON edits are reflected visually and malformed JSON is preserved until explicitly reset', async () => {
  const user = userEvent.setup()
  render(<Fixture />)
  await user.click(screen.getByRole('tab', { name: 'JSON' }))
  const editor = screen.getByRole('textbox', { name: 'Model schedules' })
  fireEvent.input(editor, {
    target: { value: JSON.stringify({ 'gpt-4o': [weekdayWindow] }) },
  })
  await user.click(screen.getByRole('tab', { name: 'Visual' }))
  expect(screen.getByRole('combobox', { name: 'Start hour' })).toHaveValue('9')
  expect(screen.getByRole('button', { name: 'Sunday' })).toHaveAttribute(
    'aria-pressed',
    'false'
  )
  await user.click(screen.getByRole('tab', { name: 'JSON' }))
  fireEvent.input(screen.getByRole('textbox', { name: 'Model schedules' }), {
    target: { value: '{broken' },
  })
  expect(
    screen.getByRole('textbox', { name: 'Model schedules' })
  ).toHaveAttribute('aria-invalid', 'true')
  await user.click(screen.getByRole('tab', { name: 'Visual' }))
  expect(
    screen.getByRole('button', { name: 'Add time window for gpt-4o' })
  ).toBeDisabled()
  expect(screen.getByLabelText('Saved schedules')).toHaveTextContent('{broken')
  await user.click(
    screen.getByRole('button', { name: 'Enable all models all day' })
  )
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByLabelText('Saved schedules')).toBeEmptyDOMElement()
})

test('disabled schedules cannot be added, edited, or removed', () => {
  render(
    <Fixture value={JSON.stringify({ 'gpt-4o': [weekdayWindow] })} disabled />
  )
  expect(
    screen.getByRole('button', { name: 'Add time window for gpt-4o' })
  ).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Monday' })).toBeDisabled()
  expect(screen.getByRole('combobox', { name: 'Start hour' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Enable all models all day' })
  ).toBeDisabled()
})

test('an overnight edit marks the time controls invalid until its end is moved to midnight', async () => {
  const user = userEvent.setup()
  render(<Fixture value={JSON.stringify({ 'gpt-4o': [weekdayWindow] })} />)
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Start hour' }),
    '20'
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'split overnight windows into two'
  )
  expect(screen.getByRole('combobox', { name: 'Start hour' })).toHaveAttribute(
    'aria-invalid',
    'true'
  )
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'End hour' }),
    '24'
  )
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByRole('combobox', { name: 'Start hour' })).toHaveAttribute(
    'aria-invalid',
    'false'
  )
})

test('removed and long model names remain searchable so stale schedules can be cleared', async () => {
  const user = userEvent.setup()
  const model = 'organization-model-with-a-long-deployment-and-version-name'
  const onChange = vi.fn()
  const view = render(
    <ModelSchedulesEditor
      value={JSON.stringify({ [model]: [weekdayWindow] })}
      onChange={onChange}
      models={[]}
    />
  )
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  await user.type(
    screen.getByRole('textbox', { name: 'Search scheduled models' }),
    'missing'
  )
  expect(screen.getByText('No matching models')).toBeVisible()
  await user.clear(
    screen.getByRole('textbox', { name: 'Search scheduled models' })
  )
  await user.click(
    screen.getByRole('button', { name: `Enable ${model} all day` })
  )
  expect(onChange).toHaveBeenCalledWith('')
  view.rerender(
    <ModelSchedulesEditor value='' onChange={onChange} models={[]} />
  )
  await waitFor(() =>
    expect(screen.getByText('Select channel models first')).toBeVisible()
  )
})

test.each([
  ['', true],
  ['{}', true],
  ['{"gpt-4o":[]}', true],
  [
    JSON.stringify({
      'gpt-4o': Array.from({ length: 64 }, (_, index) => ({
        weekday_mask: 1,
        start_minute: index,
        end_minute: index + 1,
      })),
    }),
    true,
  ],
  [
    JSON.stringify({
      'gpt-4o': [weekdayWindow, { ...weekdayWindow, weekday_mask: 65 }],
    }),
    true,
  ],
  [
    JSON.stringify({
      'gpt-4o': [
        weekdayWindow,
        { weekday_mask: 62, start_minute: 1080, end_minute: 1440 },
      ],
    }),
    true,
  ],
  [JSON.stringify({ 'gpt-4o': [weekdayWindow, weekdayWindow] }), false],
  [
    JSON.stringify({
      'gpt-4o': Array.from({ length: 65 }, (_, index) => ({
        weekday_mask: 1,
        start_minute: index,
        end_minute: index + 1,
      })),
    }),
    false,
  ],
  ['{"missing":[]}', true],
  ['{" gpt-4o":[]}', false],
  ['{"gpt-4o":null}', false],
  ['null', false],
  ['[]', false],
  ['{broken', false],
])(
  'schedule configuration %s has expected validity %s',
  (model_schedules, valid) => {
    const result = channelFormSchema.safeParse({ ...form, model_schedules })
    expect(result.success).toBe(valid)
    if (!result.success) {
      expect(result.error.issues[0].path).toEqual(['model_schedules'])
    }
  }
)

test.each([
  [{ weekday_mask: 0 }, false],
  [{ weekday_mask: 128 }, false],
  [{ weekday_mask: 1.5 }, false],
  [{ start_minute: -1 }, false],
  [{ start_minute: 10.5 }, false],
  [{ start_minute: 1440 }, false],
  [{ end_minute: 0 }, false],
  [{ end_minute: 1441 }, false],
  [{ start_minute: 1080, end_minute: 540 }, false],
  [{ start_minute: 540, end_minute: 540 }, false],
  [{ start_minute: 0, end_minute: 1440 }, true],
])('window bounds %j have expected validity %s', (overrides, valid) => {
  expect(
    channelFormSchema.safeParse({
      ...form,
      model_schedules: JSON.stringify({
        'gpt-4o': [{ ...weekdayWindow, ...overrides }],
      }),
    }).success
  ).toBe(valid)
})

test('creating, reopening, editing, and clearing schedules preserves unrelated channel settings', () => {
  const created = transformFormDataToCreatePayload({
    ...form,
    setting: '{"custom_extension":{"keep":true}}',
    model_schedules: JSON.stringify({ 'gpt-4o': [weekdayWindow] }),
  })
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
  expect(JSON.parse(defaults.model_schedules ?? '')).toEqual({
    'gpt-4o': [weekdayWindow],
  })
  const updated = transformFormDataToUpdatePayload(defaults, saved.id)
  expect(JSON.parse(updated.setting ?? '')).toMatchObject({
    custom_extension: { keep: true },
    model_schedules: { 'gpt-4o': [weekdayWindow] },
  })
  const cleared = JSON.parse(
    transformFormDataToUpdatePayload(
      { ...defaults, model_schedules: '' },
      saved.id
    ).setting ?? ''
  )
  expect(cleared).not.toHaveProperty('model_schedules')
  expect(cleared.custom_extension).toEqual({ keep: true })
})
