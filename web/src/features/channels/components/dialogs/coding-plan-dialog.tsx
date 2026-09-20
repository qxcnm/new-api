/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw } from 'lucide-react'
import {
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatDateTimeStr } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  getServerErrorMessage,
} from '@/lib/server-error-message'

import {
  getCodingPlanKeys,
  getCodingPlanQuota,
  getGLMResetCards,
  getGLMRiskStatus,
  resetGLMCard,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import type {
  CodingPlanKey,
  CodingPlanQuotaResponse,
  CodingPlanResetCard,
  CodingPlanResetCardsResponse,
  CodingPlanRiskResponse,
} from '../../types'
import { useChannels } from '../channels-provider'

type CodingPlanDialogMode = 'quota' | 'risk' | 'reset-cards'

type CodingPlanDialogProps = {
  mode: CodingPlanDialogMode
  open: boolean
  onOpenChange: (open: boolean) => void
}

type ResetSelection = {
  card: CodingPlanResetCard
  type: 'FIVE_HOUR' | 'WEEK'
}

function formatPlanDateTime(value?: string): string | null {
  if (!value?.trim()) return null
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return null
  return formatDateTimeStr(date)
}

export function CodingPlanDialog({
  mode,
  open,
  onOpenChange,
}: CodingPlanDialogProps) {
  const { t } = useTranslation()
  const { currentRow } = useChannels()
  const queryClient = useQueryClient()
  const channelId = currentRow?.id
  const needsKeySelection = Boolean(
    currentRow?.channel_info.is_plan &&
    currentRow.type === 26 &&
    currentRow.channel_info.is_multi_key &&
    ['glm-coding-plan', 'glm-coding-plan-international'].includes(
      currentRow.channel_info.plan_name
    ) &&
    mode !== 'reset-cards'
  )
  const keySelectId = useId()
  const requestId = useRef({ value: 0 })
  const [keys, setKeys] = useState<CodingPlanKey[]>([])
  const [selectedKeyIndex, setSelectedKeyIndex] = useState<number>()
  const [loading, setLoading] = useState(false)
  const [quota, setQuota] = useState<CodingPlanQuotaResponse['data']>()
  const [risk, setRisk] = useState<CodingPlanRiskResponse['data']>()
  const [resetCards, setResetCards] =
    useState<CodingPlanResetCardsResponse['data']>()
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [resetSelection, setResetSelection] = useState<ResetSelection | null>(
    null
  )
  const [resetting, setResetting] = useState(false)

  const loadData = useCallback(
    async (keyIndex?: number, reloadKeys = false) => {
      if (channelId === undefined || !open) return
      const activeRequest = ++requestId.current.value
      setLoading(true)
      setErrorMessage(null)
      setQuota(undefined)
      setRisk(undefined)
      setResetCards(undefined)
      try {
        if (needsKeySelection && (keyIndex === undefined || reloadKeys)) {
          const response = await getCodingPlanKeys(channelId)
          if (activeRequest !== requestId.current.value) return
          if (!response.success || !response.data) {
            throw createServerError(response, t('Failed to load channel keys'))
          }
          setKeys(response.data.keys)
          if (keyIndex !== undefined) {
            const selectedKey = response.data.keys.find(
              (key) => key.index === keyIndex
            )
            if (!selectedKey) {
              throw createServerError({
                message: 'The selected CodingPlan key is missing',
              })
            }
            if (!selectedKey.enabled) {
              throw createServerError({
                message: 'The selected CodingPlan key is disabled',
              })
            }
          } else {
            keyIndex = response.data.keys.find((key) => key.enabled)?.index
          }
          setSelectedKeyIndex(keyIndex)
          if (keyIndex === undefined) return
        }
        if (mode === 'quota') {
          const response = needsKeySelection
            ? await getCodingPlanQuota(channelId, keyIndex)
            : await getCodingPlanQuota(channelId)
          if (activeRequest !== requestId.current.value) return
          if (!response.success || (needsKeySelection && !response.data)) {
            throw createServerError(
              response,
              t('Failed to fetch coding plan quota')
            )
          }
          setQuota(response.data)
        } else if (mode === 'risk') {
          const response = needsKeySelection
            ? await getGLMRiskStatus(channelId, keyIndex)
            : await getGLMRiskStatus(channelId)
          if (activeRequest !== requestId.current.value) return
          if (!response.success || (needsKeySelection && !response.data)) {
            throw createServerError(
              response,
              t('Failed to fetch risk control status')
            )
          }
          setRisk(response.data)
        } else {
          const response = await getGLMResetCards(channelId)
          if (activeRequest !== requestId.current.value) return
          if (!response.success) {
            throw createServerError(response, t('Failed to fetch reset cards'))
          }
          setResetCards(response.data)
        }
      } catch (error: unknown) {
        if (activeRequest !== requestId.current.value) return
        const message = t(
          getServerErrorMessage(error, t('Failed to load coding plan data'))
        )
        setErrorMessage(message)
        handleServerError(error, message, { title: message })
      } finally {
        if (activeRequest === requestId.current.value) setLoading(false)
      }
    },
    [channelId, mode, needsKeySelection, open, t]
  )

  useEffect(() => {
    setResetSelection(null)
    setKeys([])
    setSelectedKeyIndex(undefined)
    if (!open) return
    setQuota(undefined)
    setRisk(undefined)
    setResetCards(undefined)
    setErrorMessage(null)
    void loadData()
    const requests = requestId.current
    return () => {
      requests.value++
    }
  }, [loadData, open])

  const handleReset = async () => {
    if (!currentRow || !resetSelection) return
    setResetting(true)
    try {
      const response = await resetGLMCard(currentRow.id, {
        record_id: resetSelection.card.recordId,
        reset_type: resetSelection.type,
      })
      if (!response.success) {
        throw createServerError(response, t('Failed to use reset card'))
      }
      toast.success(t('Reset card used successfully'))
      setResetSelection(null)
      await loadData()
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })
    } catch (error: unknown) {
      const message = t(
        getServerErrorMessage(error, t('Failed to use reset card'))
      )
      setErrorMessage(message)
      handleServerError(error, message, { title: message })
    } finally {
      setResetting(false)
    }
  }

  if (!currentRow) return null

  let title = t('Reset Cards')
  if (mode === 'quota') title = t('View Quota')
  if (mode === 'risk') title = t('Risk Control Status')

  const statusLabel = (status: string) => {
    if (status === 'normal') return t('Normal')
    if (status === 'risk') return t('Risk Control')
    return t('Unknown')
  }

  const quotaStatusLabel = (status?: string) => {
    if (status === 'healthy') return t('Healthy')
    if (status === 'warning') return t('Warning')
    if (status === 'critical') return t('Critical')
    if (status === 'exhausted') return t('Exhausted')
    return t('Available')
  }

  const resetCardList = (
    type: ResetSelection['type'],
    cards: CodingPlanResetCard[]
  ) => (
    <div className='space-y-2'>
      <div className='text-muted-foreground text-sm'>
        {type === 'FIVE_HOUR'
          ? t('Five-hour reset cards')
          : t('Weekly reset cards')}
      </div>
      {cards.length === 0 ? (
        <div className='text-muted-foreground rounded-md border p-3 text-sm'>
          {t('No reset cards available')}
        </div>
      ) : (
        cards.map((card) => (
          <div
            key={`${type}-${card.recordId}`}
            className='flex items-center justify-between gap-3 rounded-md border p-3'
          >
            <div className='min-w-0 text-sm'>
              <div className='font-medium'>
                {t('Reset card #{{id}}', { id: card.recordId })}
              </div>
              <div className='text-muted-foreground'>
                {t('Expires: {{date}} (local time)', {
                  date: formatPlanDateTime(card.expireTime) ?? t('Unknown'),
                })}
              </div>
              {!card.available && (
                <Badge variant='destructive'>{t('Expired')}</Badge>
              )}
            </div>
            <Button
              size='sm'
              variant='outline'
              disabled={!card.available || resetting}
              onClick={() => setResetSelection({ card, type })}
            >
              {t('Use Reset Card')}
            </Button>
          </div>
        ))
      )}
    </div>
  )

  let body: ReactNode
  if (loading) {
    body = (
      <div className='text-muted-foreground flex items-center justify-center gap-2 py-8 text-sm'>
        <LoadingState inline size='sm' message={t('Loading...')} />
      </div>
    )
  } else if (needsKeySelection && errorMessage) {
    body = (
      <ErrorState
        title={t('Request failed')}
        description={errorMessage}
        className='min-h-0 py-6'
      />
    )
  } else if (errorMessage) {
    body = (
      <Alert variant='destructive'>
        <AlertTriangle />
        <AlertTitle>{t('Request failed')}</AlertTitle>
        <AlertDescription>{errorMessage}</AlertDescription>
      </Alert>
    )
  } else if (needsKeySelection && selectedKeyIndex === undefined) {
    body = (
      <EmptyState
        title={t('No enabled keys available')}
        className='min-h-0 py-6'
      />
    )
  } else if (mode === 'quota') {
    if (quota?.credential === 'expired') {
      body = (
        <Alert variant='destructive'>
          <AlertTitle>{t('Credential expired')}</AlertTitle>
          <AlertDescription>
            {t('The CodingPlan credential has expired or is invalid.')}
          </AlertDescription>
        </Alert>
      )
    } else if (
      needsKeySelection &&
      quota?.quota_supported &&
      quota.tiers.length === 0
    ) {
      body = <EmptyState title={t('No Data')} className='min-h-0 py-6' />
    } else if (quota?.quota_supported) {
      body = (
        <div className='space-y-3'>
          {(quota.product_name || quota.plan_version) && (
            <div className='text-muted-foreground text-sm'>
              {[quota.product_name, quota.plan_version]
                .filter(Boolean)
                .join(' · ')}
            </div>
          )}
          {quota.tiers.map((tier) => {
            let tierLabel = tier.name
            if (tier.name === 'five_hour') {
              tierLabel = t('Five-hour quota')
            }
            if (tier.name === 'weekly_limit') {
              tierLabel = t('Weekly quota')
            }
            return (
              <div key={tier.name} className='space-y-2 rounded-md border p-3'>
                <div className='flex items-center justify-between gap-2'>
                  <span className='font-medium'>{tierLabel}</span>
                  <Badge
                    variant={
                      tier.status === 'critical' || tier.status === 'exhausted'
                        ? 'destructive'
                        : 'secondary'
                    }
                  >
                    {quotaStatusLabel(tier.status)}
                  </Badge>
                </div>
                <div className='text-muted-foreground text-sm'>
                  {t('Remaining: {{remaining}} / {{limit}}', {
                    remaining: tier.remaining,
                    limit: tier.limit,
                  })}
                </div>
                {tier.resets_at && (
                  <div className='text-muted-foreground text-xs'>
                    {t('Resets at: {{time}} (local time)', {
                      time: formatPlanDateTime(tier.resets_at) ?? t('Unknown'),
                    })}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )
    } else {
      body = (
        <Alert>
          <AlertTitle>{t('Quota query is not supported')}</AlertTitle>
          <AlertDescription>
            {t('This CodingPlan provider does not expose a quota endpoint.')}
          </AlertDescription>
        </Alert>
      )
    }
  } else if (mode === 'risk') {
    body = (
      <Alert variant={risk?.status === 'risk' ? 'destructive' : undefined}>
        {risk?.status === 'risk' ? <AlertTriangle /> : <CheckCircle2 />}
        <AlertTitle>{statusLabel(risk?.status ?? 'unknown')}</AlertTitle>
        <AlertDescription>{t('GLM risk control status')}</AlertDescription>
      </Alert>
    )
  } else {
    body = (
      <div className='space-y-4'>
        {resetCardList('FIVE_HOUR', resetCards?.five_hour_resets ?? [])}
        {resetCardList('WEEK', resetCards?.week_resets ?? [])}
      </div>
    )
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={title}
        description={
          <>
            {currentRow.name} · {currentRow.channel_info.plan_name}
          </>
        }
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              variant='outline'
              onClick={() => void loadData(selectedKeyIndex, needsKeySelection)}
              disabled={loading}
            >
              {loading ? (
                <Loader2 className='mr-2 size-4 animate-spin' />
              ) : (
                <RefreshCw className='mr-2 size-4' />
              )}
              {t('Refresh')}
            </Button>
            <Button
              variant='outline'
              onClick={() => onOpenChange(false)}
              disabled={loading}
            >
              {t('Close')}
            </Button>
          </>
        }
      >
        {needsKeySelection && (
          <div className='space-y-2'>
            <Label htmlFor={keySelectId}>{t('Query key')}</Label>
            <Select
              value={selectedKeyIndex ?? null}
              items={keys.map((key) => ({
                value: key.index,
                label: t('Key {{number}} · {{identifier}}', {
                  number: key.index + 1,
                  identifier: key.identifier,
                }),
              }))}
              disabled={keys.length === 0}
              onValueChange={(value) => {
                if (
                  value === null ||
                  !keys.some((key) => key.index === value && key.enabled)
                ) {
                  return
                }
                setSelectedKeyIndex(value)
                void loadData(value)
              }}
            >
              <SelectTrigger
                id={keySelectId}
                className='w-full'
                aria-describedby={`${keySelectId}-hint`}
              >
                <SelectValue placeholder={t('Select a key')} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {keys.map((key) => (
                    <SelectItem
                      key={key.index}
                      value={key.index}
                      disabled={!key.enabled}
                    >
                      {t('Key {{number}} · {{identifier}}', {
                        number: key.index + 1,
                        identifier: key.identifier,
                      })}
                      {!key.enabled && (
                        <span className='text-muted-foreground'>
                          {' '}
                          ({t('Disabled')})
                        </span>
                      )}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <p
              id={`${keySelectId}-hint`}
              className='text-muted-foreground text-xs'
            >
              {t(
                'Select a key to query its status. Refresh checks the selected key again.'
              )}
            </p>
          </div>
        )}
        {body}
      </Dialog>

      <ConfirmDialog
        open={open && resetSelection !== null}
        onOpenChange={(value) => {
          if (!value && !resetting) setResetSelection(null)
        }}
        title={t('Use Reset Card')}
        desc={t(
          'Use reset card #{{id}} now? This action will consume the selected reset card.',
          { id: resetSelection?.card.recordId ?? '' }
        )}
        confirmText={t('Use Reset Card')}
        isLoading={resetting}
        handleConfirm={() => void handleReset()}
      />
    </>
  )
}
