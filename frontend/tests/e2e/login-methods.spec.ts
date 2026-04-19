import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'

type AuthMethod = {
  id: string
  type: 'password' | 'sso'
  display_name: string
  login_url: string
}

function mockAuthMethods(page: Page, methods: AuthMethod[]) {
  return page.route('**/api/auth/methods', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ methods }),
    }),
  )
}

test('login page renders password form when only password method is advertised', async ({ page }) => {
  await mockAuthMethods(page, [
    { id: 'password', type: 'password', display_name: 'Email + password', login_url: '/api/auth/login' },
  ])

  await page.goto('/login')

  await expect(page.getByLabel('Email')).toBeVisible()
  await expect(page.getByLabel('Password')).toBeVisible()
  await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible()

  // No SSO button in this scenario.
  await expect(page.getByRole('link', { name: /sign in with/i })).toHaveCount(0)
})

test('login page renders SSO button when an SSO method is advertised', async ({ page }) => {
  await mockAuthMethods(page, [
    { id: 'password', type: 'password', display_name: 'Email + password', login_url: '/api/auth/login' },
    { id: 'okta-main', type: 'sso', display_name: 'Okta', login_url: '/api/auth/sso/okta-main/login' },
  ])

  await page.goto('/login')

  // Password form still present.
  await expect(page.getByLabel('Email')).toBeVisible()

  // SSO button present, points at the advertised login URL.
  const ssoLink = page.getByRole('link', { name: /sign in with okta/i })
  await expect(ssoLink).toBeVisible()
  await expect(ssoLink).toHaveAttribute('href', '/api/auth/sso/okta-main/login')
})

test('login page falls back to password form when methods endpoint fails', async ({ page }) => {
  await page.route('**/api/auth/methods', (route) =>
    route.fulfill({ status: 500, contentType: 'text/plain', body: 'boom' }),
  )

  await page.goto('/login')

  // Degraded but functional: password form still renders.
  await expect(page.getByLabel('Email')).toBeVisible()
  await expect(page.getByLabel('Password')).toBeVisible()
  await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible()
})
