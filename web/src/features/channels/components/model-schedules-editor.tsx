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
import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  MAX_MODEL_SCHEDULE_WINDOWS,
  parseModelSchedules,
  validateModelSchedules,
  type ModelScheduleWindow,
} from '../lib/model-schedules'

type ModelSchedulesEditorProps = {
  value: string
  onChange: (value: string) => void
  models: string[]
  disabled?: boolean
}

const HOURS = Array.from({ length: 25 }, (_, hour) => hour)
const MINUTES = Array.from({ length: 60 }, (_, minute) => minute)

function ScheduleTimeInput(props: {
  value: number
  onChange: (value: number) => void
  end?: boolean
  disabled?: boolean
  invalid?: boolean
}) {
  const { t } = useTranslation()
  const id = useId()
  const hour = Math.floor(props.value / 60)
  return (
    <Field data-invalid={props.invalid} data-disabled={props.disabled}>
      <FieldLabel htmlFor={`${id}-hour`}>
        {props.end ? t('End time') : t('Start time')}
      </FieldLabel>
      <div className='flex items-center gap-1'>
        <NativeSelect
          id={`${id}-hour`}
          aria-label={props.end ? t('End hour') : t('Start hour')}
          value={hour}
          disabled={props.disabled}
          aria-invalid={props.invalid}
          onChange={(event) => {
            const nextHour = Number(event.target.value)
            props.onChange(
              nextHour === 24 ? 1440 : nextHour * 60 + (props.value % 60)
            )
          }}
        >
          {HOURS.filter((value) => props.end || value < 24).map((value) => (
            <NativeSelectOption key={value} value={value}>
              {String(value).padStart(2, '0')}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <span aria-hidden='true'>:</span>
        <NativeSelect
          aria-label={props.end ? t('End minute') : t('Start minute')}
          value={props.value % 60}
          disabled={props.disabled || hour === 24}
          aria-invalid={props.invalid}
          onChange={(event) =>
            props.onChange(hour * 60 + Number(event.target.value))
          }
        >
          {MINUTES.map((value) => (
            <NativeSelectOption key={value} value={value}>
              {String(value).padStart(2, '0')}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>
    </Field>
  )
}

function ScheduleWindowEditor(props: {
  window: ModelScheduleWindow
  model: string
  index: number
  disabled?: boolean
  onChange: (window: ModelScheduleWindow) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const invalidTime =
    !Number.isInteger(props.window.start_minute) ||
    !Number.isInteger(props.window.end_minute) ||
    props.window.start_minute < 0 ||
    props.window.start_minute >= 1440 ||
    props.window.end_minute > 1440 ||
    props.window.end_minute <= props.window.start_minute
  const weekdays = [
    { bit: 2, label: t('Monday') },
    { bit: 4, label: t('Tuesday') },
    { bit: 8, label: t('Wednesday') },
    { bit: 16, label: t('Thursday') },
    { bit: 32, label: t('Friday') },
    { bit: 64, label: t('Saturday') },
    { bit: 1, label: t('Sunday') },
  ]
  return (
    <FieldSet className='rounded-md border p-3' disabled={props.disabled}>
      <FieldLegend variant='label' className='max-w-full break-all'>
        {t('Time window {{index}} for {{model}}', {
          index: props.index + 1,
          model: props.model,
        })}
      </FieldLegend>
      <ToggleGroup
        multiple
        variant='outline'
        spacing={1}
        size='sm'
        className='flex-wrap'
        aria-label={t('Days of the week')}
        aria-invalid={
          props.window.weekday_mask < 1 ||
          props.window.weekday_mask > 127 ||
          !Number.isInteger(props.window.weekday_mask)
        }
        disabled={props.disabled}
        value={weekdays
          .filter((day) => (props.window.weekday_mask & day.bit) !== 0)
          .map((day) => String(day.bit))}
        onValueChange={(values) =>
          props.onChange({
            ...props.window,
            weekday_mask: values.reduce(
              (mask, value) => mask | Number(value),
              0
            ),
          })
        }
      >
        {weekdays.map((day) => (
          <ToggleGroupItem key={day.bit} value={String(day.bit)}>
            {day.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={props.disabled}
          onClick={() => props.onChange({ ...props.window, weekday_mask: 127 })}
        >
          {t('Every day')}
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={props.disabled}
          onClick={() => props.onChange({ ...props.window, weekday_mask: 62 })}
        >
          {t('Weekdays')}
        </Button>
      </div>
      <FieldGroup className='gap-3'>
        <div className='grid gap-3 sm:grid-cols-2'>
          <ScheduleTimeInput
            value={props.window.start_minute}
            disabled={props.disabled}
            invalid={invalidTime}
            onChange={(start_minute) =>
              props.onChange({ ...props.window, start_minute })
            }
          />
          <ScheduleTimeInput
            value={props.window.end_minute}
            disabled={props.disabled}
            invalid={invalidTime}
            end
            onChange={(end_minute) =>
              props.onChange({ ...props.window, end_minute })
            }
          />
        </div>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='self-start'
          disabled={props.disabled}
          onClick={props.onRemove}
        >
          {t('Remove')}
        </Button>
      </FieldGroup>
    </FieldSet>
  )
}

export function ModelSchedulesEditor(props: ModelSchedulesEditorProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const schedules = useMemo(
    () => parseModelSchedules(props.value),
    [props.value]
  )
  const models = useMemo(
    () => [...new Set([...props.models, ...Object.keys(schedules ?? {})])],
    [props.models, schedules]
  )
  const filteredModels = models.filter((model) =>
    model.toLowerCase().includes(search.trim().toLowerCase())
  )
  const error = validateModelSchedules(props.value)

  const updateWindows = (
    model: string,
    windows: ModelScheduleWindow[]
  ): void => {
    if (!schedules || props.disabled) return
    const next = { ...schedules, [model]: windows }
    if (windows.length === 0) delete next[model]
    props.onChange(
      Object.keys(next).length ? JSON.stringify(next, null, 2) : ''
    )
  }

  return (
    <Tabs defaultValue='visual' className='space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <TabsList>
          <TabsTrigger value='visual'>{t('Visual')}</TabsTrigger>
          <TabsTrigger value='json'>{t('JSON')}</TabsTrigger>
        </TabsList>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={props.disabled || !props.value.trim()}
          onClick={() => props.onChange('')}
        >
          {t('Enable all models all day')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Weekly schedules use Beijing time (UTC+8). Models without time windows are available all day. Windows include the start and exclude the end; use 24:00 for midnight and split overnight periods into two windows.'
        )}
      </p>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription>{t(error)}</AlertDescription>
        </Alert>
      )}
      <TabsContent value='visual' className='space-y-3'>
        {models.length > 0 ? (
          <>
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('Search models...')}
              aria-label={t('Search scheduled models')}
            />
            <div className='max-h-96 space-y-3 overflow-y-auto pr-1'>
              {filteredModels.map((model) => {
                const windows =
                  schedules && Object.hasOwn(schedules, model)
                    ? schedules[model]
                    : []
                return (
                  <FieldSet
                    key={model}
                    className='min-w-0 rounded-md border p-3'
                  >
                    <FieldLegend
                      className='max-w-full break-all'
                      variant='label'
                    >
                      {model}
                    </FieldLegend>
                    <div className='flex flex-wrap items-center gap-2'>
                      {windows.length === 0 && (
                        <Badge variant='secondary'>{t('All day')}</Badge>
                      )}
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        disabled={
                          props.disabled ||
                          !schedules ||
                          windows.length >= MAX_MODEL_SCHEDULE_WINDOWS
                        }
                        aria-label={t('Add time window for {{model}}', {
                          model,
                        })}
                        onClick={() =>
                          updateWindows(model, [
                            ...windows,
                            {
                              weekday_mask: 127,
                              start_minute: 540,
                              end_minute: 1080,
                            },
                          ])
                        }
                      >
                        {t('Add time window')}
                      </Button>
                      {Object.hasOwn(schedules ?? {}, model) && (
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          disabled={props.disabled}
                          aria-label={t('Enable {{model}} all day', { model })}
                          onClick={() => updateWindows(model, [])}
                        >
                          {t('All day')}
                        </Button>
                      )}
                    </div>
                    {windows.map((window, index) => (
                      <ScheduleWindowEditor
                        // Windows have no IDs and all fields are controlled; preserve focus while editing.
                        // eslint-disable-next-line react/no-array-index-key
                        key={index}
                        window={window}
                        index={index}
                        model={model}
                        disabled={props.disabled}
                        onChange={(next) =>
                          updateWindows(
                            model,
                            windows.map((item, itemIndex) =>
                              itemIndex === index ? next : item
                            )
                          )
                        }
                        onRemove={() =>
                          updateWindows(
                            model,
                            windows.filter(
                              (_, itemIndex) => itemIndex !== index
                            )
                          )
                        }
                      />
                    ))}
                  </FieldSet>
                )
              })}
              {filteredModels.length === 0 && (
                <EmptyState
                  title={t('No matching models')}
                  className='min-h-32'
                />
              )}
            </div>
          </>
        ) : (
          <EmptyState
            title={t('Select channel models first')}
            className='min-h-32'
          />
        )}
      </TabsContent>
      <TabsContent value='json'>
        <JsonCodeEditor
          value={props.value}
          onChange={props.onChange}
          disabled={props.disabled}
          ariaLabel={t('Model schedules')}
          aria-invalid={Boolean(error)}
          placeholder='{"gpt-4o":[{"weekday_mask":62,"start_minute":540,"end_minute":1080}]}'
        />
      </TabsContent>
    </Tabs>
  )
}
