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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { ModelGroupsEditor } from '../model-groups-editor'

function Fixture(props: { value?: string; disabled?: boolean }) {
  const [value, setValue] = useState(props.value ?? '')
  return (
    <>
      <ModelGroupsEditor
        value={value}
        onChange={setValue}
        models={['gpt-4o', 'claude-3']}
        groups={['default', 'vip']}
        disabled={props.disabled}
      />
      <output aria-label='Saved bindings'>{value}</output>
    </>
  )
}

test('changing one model group with the keyboard keeps other models inherited', async () => {
  const user = userEvent.setup()
  render(<Fixture />)
  const input = screen.getByRole('combobox', { name: 'Groups for gpt-4o' })
  await user.click(input)
  await user.keyboard('{Backspace}{Escape}')

  expect(
    JSON.parse(screen.getByLabelText('Saved bindings').textContent ?? '')
  ).toEqual({
    'gpt-4o': ['default'],
  })
  expect(screen.getAllByText('Inherited')).toHaveLength(1)
  expect(
    screen.getByRole('combobox', { name: 'Groups for claude-3' })
  ).toBeVisible()
})

test('removing the final group shows an error until the model is reset to inherited groups', async () => {
  const user = userEvent.setup()
  render(<Fixture value='{"gpt-4o":["vip"]}' />)
  await user.click(screen.getByRole('combobox', { name: 'Groups for gpt-4o' }))
  await user.click(screen.getByRole('option', { name: 'vip' }))
  await user.keyboard('{Escape}')

  expect(screen.getByRole('alert')).toHaveTextContent(
    'Select at least one group for each model, or use channel groups'
  )
  expect(
    JSON.parse(screen.getByLabelText('Saved bindings').textContent ?? '')
  ).toEqual({
    'gpt-4o': [],
  })
  await user.click(
    screen.getByRole('button', { name: 'Use channel groups for gpt-4o' })
  )
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(screen.getByLabelText('Saved bindings')).toBeEmptyDOMElement()
  expect(screen.getAllByText('Inherited')).toHaveLength(2)
})

test('invalid JSON remains editable and cannot be replaced by a visual selection', async () => {
  const user = userEvent.setup()
  render(<Fixture value='{"gpt-4o":' />)
  expect(
    screen.getByRole('combobox', { name: 'Groups for gpt-4o' })
  ).toBeDisabled()
  await user.click(screen.getByRole('tab', { name: 'JSON' }))
  const editor = screen.getByRole('textbox', { name: 'Model groups' })
  expect(editor).toHaveValue('{"gpt-4o":')
  expect(editor).toHaveAttribute('aria-invalid', 'true')
  await user.click(
    screen.getByRole('button', { name: 'Use channel groups for all models' })
  )
  expect(editor).toHaveValue('')
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

test('search filters long model names and displays a visible empty result', async () => {
  const user = userEvent.setup()
  const longModel =
    'organization-reasoning-model-with-a-long-version-and-deployment-name'
  render(
    <ModelGroupsEditor
      value=''
      onChange={vi.fn()}
      models={[longModel, 'gpt-4o']}
      groups={['vip']}
    />
  )
  const search = screen.getByRole('textbox', { name: 'Search models...' })
  await user.type(search, 'deployment')
  expect(
    screen.getByRole('combobox', { name: `Groups for ${longModel}` })
  ).toBeVisible()
  expect(
    screen.queryByRole('combobox', { name: 'Groups for gpt-4o' })
  ).not.toBeInTheDocument()
  await user.clear(search)
  await user.type(search, 'missing')
  await waitFor(() =>
    expect(screen.getByText('No matching models')).toBeVisible()
  )
})

test('disabled bindings cannot be changed or reset', () => {
  render(<Fixture value='{"gpt-4o":["vip"]}' disabled />)
  expect(
    screen.getByRole('combobox', { name: 'Groups for gpt-4o' })
  ).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Use channel groups for gpt-4o' })
  ).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Use channel groups for all models' })
  ).toBeDisabled()
})

test('removed models remain visible for fixing stale bindings and external updates do not emit edits', async () => {
  const onChange = vi.fn()
  const view = render(
    <ModelGroupsEditor
      value='{"removed-model":["vip"]}'
      onChange={onChange}
      models={[]}
      groups={['vip']}
    />
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Model group bindings must reference models in this channel'
  )
  expect(
    screen.getByRole('combobox', { name: 'Groups for removed-model' })
  ).toBeVisible()
  view.rerender(
    <ModelGroupsEditor
      value=''
      onChange={onChange}
      models={[]}
      groups={['vip']}
    />
  )
  await waitFor(() =>
    expect(screen.getByText('Select channel models first')).toBeVisible()
  )
  expect(onChange).not.toHaveBeenCalled()
})
