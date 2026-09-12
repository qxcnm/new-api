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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { PublicHeader } from '../public-header'

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    // Registration remains available even when self-use mode is enabled.
    status: { register_enabled: true, self_use_mode_enabled: true },
  }),
}))

vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({
    systemName: 'Test',
    logo: '',
    loading: false,
    logoLoaded: true,
  }),
}))

vi.mock('@/hooks/use-top-nav-links', () => ({
  useTopNavLinks: () => [],
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: () => ({ auth: { user: null } }),
}))

vi.mock('@/hooks/use-notifications', () => ({
  useNotifications: () => ({
    popoverOpen: false,
    setPopoverOpen: vi.fn(),
    unreadCount: 0,
    activeTab: 'notice',
    setActiveTab: vi.fn(),
    notice: null,
    announcements: [],
    loading: false,
  }),
}))

vi.mock('@/components/language-switcher', () => ({
  LanguageSwitcher: () => null,
}))

vi.mock('@/components/theme-switch', () => ({
  ThemeSwitch: () => null,
}))

vi.mock('@/components/notification-popover', () => ({
  NotificationPopover: () => null,
}))

vi.mock('@/components/profile-dropdown', () => ({
  ProfileDropdown: () => null,
}))

function renderHeader() {
  const root = createRootRoute({ component: Outlet })
  const routes = [
    createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => <PublicHeader />,
    }),
    createRoute({
      getParentRoute: () => root,
      path: '/sign-up',
      component: () => <div>Sign-up page</div>,
    }),
    createRoute({
      getParentRoute: () => root,
      path: '/forgot-password',
      component: () => <div>Forgot-password page</div>,
    }),
  ]
  const router = createRouter({
    routeTree: root.addChildren(routes),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return render(<RouterProvider router={router} />)
}

afterEach(() => {
  vi.clearAllMocks()
})

it('links the public header sign-up button to the registration page', async () => {
  renderHeader()

  const signUp = await screen.findByRole('button', { name: 'Sign up' })
  expect(signUp).toHaveAttribute('href', '/sign-up')
  expect(signUp).not.toHaveAttribute('href', '/forgot-password')
})
