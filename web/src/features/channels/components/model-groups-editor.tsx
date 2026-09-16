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
import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { parseModelGroups, validateModelGroups } from '../lib/model-groups'

type ModelGroupsEditorProps = {
  value: string
  onChange: (value: string) => void
  models: string[]
  groups: string[]
  disabled?: boolean
}

export function ModelGroupsEditor(props: ModelGroupsEditorProps) {
  const { t } = useTranslation()
  const id = useId()
  const [search, setSearch] = useState('')
  const bindings = useMemo(() => parseModelGroups(props.value), [props.value])
  const options = useMemo(
    () => props.groups.map((group) => ({ label: group, value: group })),
    [props.groups]
  )
  const models = useMemo(
    () => [...new Set([...props.models, ...Object.keys(bindings ?? {})])],
    [props.models, bindings]
  )
  const filteredModels = models.filter((model) =>
    model.toLowerCase().includes(search.trim().toLowerCase())
  )
  const error = validateModelGroups(props.value, props.models, props.groups)

  const updateBinding = (model: string, groups?: string[]): void => {
    if (!bindings) return
    const next =
      groups === undefined ? { ...bindings } : { ...bindings, [model]: groups }
    if (groups === undefined) {
      delete next[model]
    }
    props.onChange(
      Object.keys(next).length ? JSON.stringify(next, null, 2) : ''
    )
  }

  return (
    <div className='space-y-3'>
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
            {t('Use channel groups for all models')}
          </Button>
        </div>
        {error && (
          <Alert variant='destructive'>
            <AlertDescription>{t(error)}</AlertDescription>
          </Alert>
        )}
        <TabsContent value='visual' className='space-y-3'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Each model inherits channel groups until you select its own groups. Group pricing applies to requests using that group.'
            )}
          </p>
          {models.length > 0 ? (
            <>
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search models...')}
                aria-label={t('Search models...')}
              />
              <div className='max-h-96 space-y-3 overflow-y-auto pr-1'>
                {filteredModels.map((model, index) => {
                  const inherited = !Object.hasOwn(bindings ?? {}, model)
                  const selected = inherited
                    ? props.groups
                    : (bindings?.[model] ?? [])
                  const inputId = `${id}-${index}`
                  return (
                    <div
                      key={model}
                      className='space-y-2 rounded-md border p-3'
                    >
                      <div className='flex flex-wrap items-center justify-between gap-2'>
                        <Label htmlFor={inputId} className='min-w-0 break-all'>
                          {t('Groups for {{model}}', { model })}
                        </Label>
                        {inherited ? (
                          <Badge variant='secondary'>{t('Inherited')}</Badge>
                        ) : (
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            disabled={props.disabled}
                            aria-label={t('Use channel groups for {{model}}', {
                              model,
                            })}
                            onClick={() => updateBinding(model)}
                          >
                            {t('Use channel groups')}
                          </Button>
                        )}
                      </div>
                      <MultiSelect
                        id={inputId}
                        options={options}
                        selected={selected}
                        onChange={(groups) => updateBinding(model, groups)}
                        disabled={props.disabled || !bindings}
                        maxVisibleChips={4}
                        placeholder={t('Groups for {{model}}', { model })}
                      />
                    </div>
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
            ariaLabel={t('Model groups')}
            aria-invalid={Boolean(error)}
            placeholder='{"gpt-4o":["default"],"claude-3":["vip"]}'
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
