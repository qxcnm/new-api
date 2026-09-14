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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import type { TopupInfo } from '../../types'
import { RechargeFormCard } from '../recharge-form-card'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: { percent?: number }) =>
      key.replace('{{percent}}', String(values?.percent ?? '')),
  }),
}))

afterEach(() => {
  cleanup()
})

function renderRechargeCard(topupInfo: TopupInfo, amount = 100) {
  return render(
    <RechargeFormCard
      topupInfo={topupInfo}
      presetAmounts={[]}
      selectedPreset={null}
      onSelectPreset={vi.fn()}
      topupAmount={amount}
      onTopupAmountChange={vi.fn()}
      paymentAmount={amount}
      calculating={false}
      onPaymentMethodSelect={vi.fn()}
      paymentLoading={null}
      redemptionCode=''
      onRedemptionCodeChange={vi.fn()}
      onRedeem={vi.fn()}
      redeeming={false}
    />
  )
}

test('shows the matching amount bonus percentage on the top-up form', () => {
  renderRechargeCard({
    enable_online_topup: true,
    enable_stripe_topup: false,
    pay_methods: [{ name: 'Alipay', type: 'alipay' }],
    min_topup: 1,
    stripe_min_topup: 1,
    amount_options: [],
    discount: {},
    amount_bonus: { 100: 10 },
  })

  expect(
    screen.getByText('Top-up bonus: 10% extra balance')
  ).toBeInTheDocument()
})

test('does not show a bonus message below the configured amount threshold', () => {
  renderRechargeCard(
    {
      enable_online_topup: true,
      enable_stripe_topup: false,
      pay_methods: [{ name: 'Alipay', type: 'alipay' }],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      amount_bonus: { 100: 10 },
    },
    99
  )

  expect(screen.queryByText(/Top-up bonus:/)).not.toBeInTheDocument()
})

test('uses the highest matching amount bonus tier', () => {
  renderRechargeCard(
    {
      enable_online_topup: true,
      enable_stripe_topup: false,
      pay_methods: [{ name: 'Alipay', type: 'alipay' }],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
      amount_bonus: { 100: 10, 500: 20 },
    },
    500
  )

  expect(
    screen.getByText('Top-up bonus: 20% extra balance')
  ).toBeInTheDocument()
})
