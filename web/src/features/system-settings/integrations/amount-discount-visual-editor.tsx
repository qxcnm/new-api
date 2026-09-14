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
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import {
  AmountDiscountDialog,
  type AmountTierKind,
  type AmountDiscountData,
} from './amount-discount-dialog'

type AmountDiscountVisualEditorProps = {
  value: string
  onChange: (value: string) => void
  kind?: AmountTierKind
}

export function AmountDiscountVisualEditor({
  value,
  onChange,
  kind = 'discount',
}: AmountDiscountVisualEditorProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<AmountDiscountData | null>(null)

  const tiers = useMemo(() => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      validatorMessage:
        kind === 'discount'
          ? 'Amount discount must be a JSON object'
          : 'Amount bonus must be a JSON object',
      context: kind === 'discount' ? 'amount discounts' : 'amount bonuses',
    })

    return Object.entries(parsed)
      .map(([amount, rate]) => ({
        amount: Number.parseInt(amount, 10),
        discountRate:
          typeof rate === 'number' ? rate : Number.parseFloat(String(rate)),
      }))
      .filter(
        (item) => !Number.isNaN(item.amount) && !Number.isNaN(item.discountRate)
      )
      .sort((a, b) => a.amount - b.amount)
  }, [kind, value])

  const handleSave = (data: AmountDiscountData) => {
    const tierObject = safeJsonParseWithValidation<Record<string, unknown>>(
      value,
      {
        fallback: {},
        validator: isObjectRecord,
        silent: true,
      }
    )

    if (editData && editData.amount !== data.amount) {
      delete tierObject[editData.amount.toString()]
    }

    tierObject[data.amount.toString()] = data.discountRate

    onChange(JSON.stringify(tierObject, null, 2))
  }

  const handleDelete = (amount: number) => {
    const tierObject = safeJsonParseWithValidation<Record<string, unknown>>(
      value,
      {
        fallback: {},
        validator: isObjectRecord,
        silent: true,
      }
    )

    delete tierObject[amount.toString()]

    onChange(JSON.stringify(tierObject, null, 2))
  }

  const handleEdit = (tier: AmountDiscountData) => {
    setEditData(tier)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  const formatPercentage = (rate: number) => {
    if (kind === 'bonus') return `${rate}%`
    if (rate >= 1) return '0%'
    const discount = Math.round((1 - rate) * 100)
    return `${discount}%`
  }

  const isDiscount = kind === 'discount'
  const tierLabel = isDiscount ? t('off') : t('Bonus')
  const description = isDiscount
    ? t('Configure discount rates based on recharge amounts')
    : t('Configure bonus percentages based on recharge amounts')
  const addLabel = isDiscount ? t('Add discount tier') : t('Add bonus tier')
  const emptyLabel = isDiscount
    ? t(
        'No discount tiers configured. Click "Add discount tier" to get started.'
      )
    : t('No bonus tiers configured. Click "Add bonus tier" to get started.')

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <p className='text-muted-foreground text-sm'>{description}</p>
        <Button
          type='button'
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            handleAdd()
          }}
          size='sm'
          className='w-full sm:w-auto'
        >
          <Plus className='h-4 w-4 sm:mr-2' />
          <span className='sm:inline'>{addLabel}</span>
        </Button>
      </div>

      {tiers.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {emptyLabel}
        </div>
      ) : (
        <div className='rounded-md border'>
          {/* Desktop table view */}
          <StaticDataTable
            className='hidden rounded-none border-0 sm:block'
            data={tiers}
            getRowKey={(tier) => tier.amount}
            columns={[
              {
                id: 'amount',
                header: t('Recharge Amount'),
                cell: (tier) => (
                  <span className='font-mono text-sm'>${tier.amount}</span>
                ),
              },
              {
                id: 'tier-rate',
                header: isDiscount ? t('Discount Rate') : t('Bonus Percentage'),
                cell: (tier) => (
                  <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
                    {isDiscount
                      ? tier.discountRate.toFixed(2)
                      : `${tier.discountRate}%`}
                  </code>
                ),
              },
              {
                id: 'tier-value',
                header: isDiscount ? t('Discount') : t('Bonus'),
                cell: (tier) => (
                  <StatusBadge
                    variant={
                      isDiscount && tier.discountRate >= 1 ? 'neutral' : 'info'
                    }
                    className='font-mono'
                    copyable={false}
                  >
                    {formatPercentage(tier.discountRate)} {tierLabel}
                  </StatusBadge>
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'text-right',
                cellClassName: 'text-right',
                cell: (tier) => (
                  <StaticRowActions
                    editLabel={t('Edit')}
                    deleteLabel={t('Delete')}
                    menuLabel={t('Open menu')}
                    onEdit={() => handleEdit(tier)}
                    onDelete={() => handleDelete(tier.amount)}
                  />
                ),
              },
            ]}
          />

          {/* Mobile card view */}
          <div className='divide-y sm:hidden'>
            {tiers.map((tier) => (
              <div key={tier.amount} className='p-4'>
                <div className='mb-3 flex items-start justify-between'>
                  <div className='flex-1'>
                    <div className='mb-2 font-mono text-base font-medium'>
                      ${tier.amount}
                    </div>
                    <StatusBadge
                      variant={
                        isDiscount && tier.discountRate >= 1
                          ? 'neutral'
                          : 'info'
                      }
                      className='font-mono'
                      copyable={false}
                    >
                      {formatPercentage(tier.discountRate)} {tierLabel}
                    </StatusBadge>
                  </div>
                  <div className='flex gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        handleEdit(tier)
                      }}
                    >
                      <Pencil className='h-4 w-4' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        handleDelete(tier.amount)
                      }}
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </div>
                </div>
                <div className='text-sm'>
                  <span className='text-muted-foreground'>
                    {isDiscount
                      ? t('Discount Rate:')
                      : t('Bonus Percentage:')}{' '}
                  </span>
                  <code className='bg-muted rounded px-1.5 py-0.5 text-xs'>
                    {isDiscount
                      ? tier.discountRate.toFixed(2)
                      : `${tier.discountRate}%`}
                  </code>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      <AmountDiscountDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
        kind={kind}
      />
    </div>
  )
}
