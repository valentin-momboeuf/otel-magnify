import { randomBytes, randomUUID } from 'node:crypto'

import type { Page, Route } from '@playwright/test'

import { expect, mockCapabilities, mockMe, test } from './fixtures'

test.use({
  trace: 'off',
  screenshot: 'off',
  video: 'off',
})

const adminGroup = {
  id: 'grp_system_administrator',
  name: 'administrator' as const,
  role: 'administrator' as const,
  is_system: true,
  created_at: '2026-07-23T08:00:00Z',
}

const editorGroup = {
  id: 'grp_system_editor',
  name: 'editor' as const,
  role: 'editor' as const,
  is_system: true,
  created_at: '2026-07-23T08:00:00Z',
}

const activeToken = {
  id: '11111111-1111-4111-8111-111111111111',
  name: 'production collectors',
  description: 'Collector and Supervisor fleet',
  team: 'platform',
  environment: 'production',
  created_at: '2026-07-23T08:00:00Z',
  created_by: 'admin@example.com',
  expires_at: '2026-08-23T08:00:00Z',
  last_used_at: '2026-07-23T09:00:00Z',
  status: 'active',
}

const expiredByServerToken = {
  ...activeToken,
  id: '22222222-2222-4222-8222-222222222222',
  name: 'server-expired token',
  expires_at: '2099-08-23T08:00:00Z',
  last_used_at: undefined,
  status: 'expired',
}

const revokedToken = {
  ...activeToken,
  id: '33333333-3333-4333-8333-333333333333',
  name: 'revoked token',
  expires_at: undefined,
  revoked_at: '2026-07-23T10:00:00Z',
  revoked_by: 'admin@example.com',
  status: 'revoked',
}

function runtimeCredential(): string {
  return `ompt_${randomUUID()}.${randomBytes(32).toString('base64url')}`
}

async function prepareAdmin(page: Page): Promise<void> {
  await mockMe(page, { groups: [adminGroup] })
  await mockCapabilities(page, {})
}

async function mockTokenList(
  page: Page,
  tokens: unknown[] | (() => unknown[]),
  onGet?: () => void,
): Promise<void> {
  await page.route('**/api/v1/opamp/tokens', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    onGet?.()
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ tokens: typeof tokens === 'function' ? tokens() : tokens }),
    })
  })
}

async function createToken(page: Page, name = 'production collectors'): Promise<void> {
  await page.getByRole('button', { name: 'Create token' }).click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create', exact: true }).click()
}

async function routeCreateSuccess(
  page: Page,
  metadata = activeToken,
  inspectRequest?: (body: Record<string, unknown>) => void,
): Promise<void> {
  await page.route('**/api/v1/opamp/tokens', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback()
      return
    }
    inspectRequest?.(route.request().postDataJSON() as Record<string, unknown>)
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ token: metadata, value: runtimeCredential() }),
    })
  })
}

async function expectCredentialElementMatchesFormat(page: Page): Promise<void> {
  const matches = await page
    .getByLabel('One-shot token value')
    .evaluate((element) =>
      /^ompt_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}$/.test(
        (element as HTMLInputElement).value,
      ),
    )
  expect(matches).toBe(true)
}

async function expectCredentialAbsentFromStorage(page: Page): Promise<void> {
  const storageContainsCredential = await page.evaluate(() => {
    const pattern =
      /ompt_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}/
    return [localStorage, sessionStorage].some((storage) =>
      Array.from({ length: storage.length }, (_, index) => {
        const key = storage.key(index) ?? ''
        return pattern.test(key) || pattern.test(storage.getItem(key) ?? '')
      }).some(Boolean),
    )
  })
  expect(storageContainsCredential).toBe(false)
}

test.describe('OpAMP token administration access and metadata', () => {
  test('shows the Community section with empty capabilities and explains the empty state', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    let listGets = 0
    await mockTokenList(page, [], () => {
      listGets += 1
    })

    await page.goto('/admin')
    await page.getByRole('link', { name: /OpAMP tokens/i }).click()

    await expect(page).toHaveURL(/\/admin\/opamp\/tokens$/)
    await expect(page.getByText(/no agent can authenticate until a token exists/i)).toBeVisible()
    await expect.poll(() => listGets).toBeGreaterThan(0)
  })

  test('refuses an editor without issuing a token-list request', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [editorGroup] })
    await mockCapabilities(page, {})
    let listGets = 0
    await mockTokenList(page, [], () => {
      listGets += 1
    })

    await page.goto('/admin/opamp/tokens')

    // eslint-disable-next-line security/detect-unsafe-regex -- bounded literal pattern over Playwright-supplied page.url(), with no user-controlled input
    await expect(page).toHaveURL(/^http:\/\/localhost:\d+\/?(?:#.*)?$/)
    expect(listGets).toBe(0)
  })

  test('renders complete metadata and trusts every server status', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [activeToken, expiredByServerToken, revokedToken])

    await page.goto('/admin/opamp/tokens')

    const table = page.getByTestId('opamp-token-table')
    await expect(table).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Name' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Team' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Environment' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Created' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Expires' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Last used' })).toBeVisible()
    await expect(table.getByRole('columnheader', { name: 'Status' })).toBeVisible()
    const activeRow = page.getByTestId(`opamp-token-row-${activeToken.id}`)
    await expect(activeRow.getByText(activeToken.description)).toBeVisible()
    await expect(activeRow.getByText(activeToken.created_by)).toBeVisible()
    await expect(activeRow.getByText(activeToken.id)).toBeVisible()
    await expect(
      page.getByTestId(`opamp-token-row-${expiredByServerToken.id}`).getByText('expired', {
        exact: true,
      }),
    ).toBeVisible()
    await expect(
      page.getByTestId(`opamp-token-row-${revokedToken.id}`).getByText('revoked', {
        exact: true,
      }),
    ).toBeVisible()
    await expect(
      page.getByTestId(`opamp-token-row-${expiredByServerToken.id}`).getByText('—', {
        exact: true,
      }),
    ).toBeVisible()
  })

  test('shows a safe list error and retries', async ({ loggedInPage: page }) => {
    await prepareAdmin(page)
    let allowSuccess = false
    await page.route('**/api/v1/opamp/tokens', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.fallback()
        return
      }
      if (!allowSuccess) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'private backend detail' }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ tokens: [] }),
      })
    })

    await page.goto('/admin/opamp/tokens')

    const alert = page.getByRole('alert')
    await expect(alert).toContainText('Unable to load OpAMP tokens')
    await expect(alert).not.toContainText('private backend detail')
    allowSuccess = true
    await alert.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByText(/no agent can authenticate until a token exists/i)).toBeVisible()
  })
})

test.describe('OpAMP token one-shot creation', () => {
  test('sends the minimal payload and clears the one-shot value only on acknowledgement', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [])
    let payload: Record<string, unknown> | undefined
    await routeCreateSuccess(page, activeToken, (body) => {
      payload = body
    })
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText(value: string) {
            const windowWithCopyState = window as typeof window & {
              __credentialCopyWasValid?: boolean
            }
            windowWithCopyState.__credentialCopyWasValid =
              /^ompt_[0-9a-f-]+\.[A-Za-z0-9_-]{43}$/.test(value)
            return Promise.resolve()
          },
        },
      })
    })

    await page.goto('/admin/opamp/tokens')
    await createToken(page)

    await expect.poll(() => payload).toEqual({ name: 'production collectors' })
    await expect(page.getByText('CREATED')).toBeVisible()
    await expect(page.getByText('VISIBLE ONCE')).toBeVisible()
    await expect(page.getByText('CLEARED ON CLOSE')).toBeVisible()
    await expectCredentialElementMatchesFormat(page)

    await page.getByRole('button', { name: 'Copy token' }).click()
    const clipboardWriteWasValid = await page.evaluate(
      () =>
        (window as typeof window & { __credentialCopyWasValid?: boolean })
          .__credentialCopyWasValid === true,
    )
    expect(clipboardWriteWasValid).toBe(true)
    await expect(page.getByText('Token copied')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByLabel('One-shot token value')).toBeVisible()
    await page
      .locator('.opamp-token-dialog-backdrop')
      .click({ position: { x: 5, y: 5 }, force: true })
    await expect(page.getByLabel('One-shot token value')).toBeVisible()

    await page.getByRole('button', { name: 'I have saved this token' }).click()
    await expect(page.getByLabel('One-shot token value')).toHaveCount(0)
    await expectCredentialAbsentFromStorage(page)

    await page.reload()
    await expect(page.getByLabel('One-shot token value')).toHaveCount(0)
    await expectCredentialAbsentFromStorage(page)
  })

  test('sends the complete payload and converts datetime-local to RFC3339', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [])
    let payload: Record<string, unknown> | undefined
    await routeCreateSuccess(page, activeToken, (body) => {
      payload = body
    })

    await page.goto('/admin/opamp/tokens')
    await page.getByRole('button', { name: 'Create token' }).click()
    await page.getByLabel('Name').fill('supervisor staging')
    await page.getByLabel('Description').fill('Supervisor staging rollout')
    await page.getByLabel('Team').fill('observability')
    await page.getByLabel('Environment').fill('staging')
    await page.getByLabel('Expires at').fill('2026-08-23T10:30')
    await page.getByRole('button', { name: 'Create', exact: true }).click()

    await expect
      .poll(() => payload)
      .toEqual({
        name: 'supervisor staging',
        description: 'Supervisor staging rollout',
        team: 'observability',
        environment: 'staging',
        expires_at: new Date('2026-08-23T10:30').toISOString(),
      })
    await expectCredentialElementMatchesFormat(page)
  })

  test('keeps controlled form values and shows safe validation after 400', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [])
    await page.route('**/api/v1/opamp/tokens', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback()
        return
      }
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'unsafe validation details' }),
      })
    })

    await page.goto('/admin/opamp/tokens')
    await createToken(page, 'retained form name')

    await expect(page.getByRole('alert')).toContainText('Check the token details and try again')
    await expect(page.getByRole('alert')).not.toContainText('unsafe validation details')
    await expect(page.getByLabel('Name')).toHaveValue('retained form name')
  })

  test('distinguishes oversized and ambiguous create failures without exposing response bodies', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [])
    let attempt = 0
    await page.route('**/api/v1/opamp/tokens', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback()
        return
      }
      attempt += 1
      if (attempt === 1) {
        await route.fulfill({
          status: 413,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'unsafe oversized details' }),
        })
        return
      }
      await route.abort('connectionfailed')
    })

    await page.goto('/admin/opamp/tokens')
    await createToken(page, 'oversized')
    await expect(page.getByRole('alert')).toContainText('Token request is too large')
    await expect(page.getByRole('alert')).not.toContainText('unsafe oversized details')

    await page.getByRole('button', { name: 'Create', exact: true }).click()
    const alert = page.getByRole('alert')
    await expect(alert).toContainText('The create outcome is unknown')
    await expect(alert).toContainText('revoke every suspect irretrievable credential')
    await expect(page.getByLabel('Name')).toHaveValue('oversized')
  })

  test('selects the credential for manual copy when clipboard access fails', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [])
    await routeCreateSuccess(page)
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText() {
            return Promise.reject(new Error('clipboard unavailable'))
          },
        },
      })
    })

    await page.goto('/admin/opamp/tokens')
    await createToken(page)
    await page.getByRole('button', { name: 'Copy token' }).click()

    await expect(page.getByRole('alert')).toContainText('Copy failed. The token is selected')
    const fullySelected = await page.getByLabel('One-shot token value').evaluate((element) => {
      const input = element as HTMLInputElement
      return input.selectionStart === 0 && input.selectionEnd === input.value.length
    })
    expect(fullySelected).toBe(true)
  })
})

test.describe('OpAMP token reconciliation and revocation', () => {
  test('reconciles create commit-unknown using only the exact public token ID', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    let listAttempt = 0
    await mockTokenList(page, () => {
      listAttempt += 1
      return listAttempt === 1 ? [] : [activeToken]
    })
    await page.route('**/api/v1/opamp/tokens', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fallback()
        return
      }
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'unsafe persistence detail',
          side_effect_status: 'unknown',
          token_id: activeToken.id,
        }),
      })
    })
    let revokedPublicId: string | undefined
    await page.route('**/api/v1/opamp/tokens/*/revoke', async (route) => {
      revokedPublicId = publicIdFromRevokeRoute(route)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ token: revokedToken, disconnected_connections: 0 }),
      })
    })

    await page.goto('/admin/opamp/tokens')
    await createToken(page, 'unknown outcome')

    await expect(page.getByLabel('One-shot token value')).toHaveCount(0)
    await expect(page.getByRole('alert')).toContainText('The server may have created this token')
    await expect(page.getByRole('alert')).not.toContainText('unsafe persistence detail')
    await page.getByRole('button', { name: 'Revoke affected token' }).click()
    await page.getByRole('button', { name: 'Confirm revoke' }).click()
    await expect.poll(() => revokedPublicId).toBe(activeToken.id)
  })

  test('refreshes and offers retry after a revoke commit-unknown while the token remains active', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    await mockTokenList(page, [activeToken])
    let revokeAttempts = 0
    await page.route('**/api/v1/opamp/tokens/*/revoke', async (route) => {
      revokeAttempts += 1
      if (revokeAttempts === 1) {
        await route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: JSON.stringify({
            error: 'unsafe disconnect detail',
            side_effect_status: 'unknown',
            token_id: activeToken.id,
          }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ token: revokedToken, disconnected_connections: 0 }),
      })
    })

    await page.goto('/admin/opamp/tokens')
    await page
      .getByTestId(`opamp-token-row-${activeToken.id}`)
      .getByRole('button', { name: 'Revoke' })
      .click()
    await page.getByRole('button', { name: 'Confirm revoke' }).click()

    const alert = page.getByRole('alert')
    await expect(alert).toContainText('Fail-closed disconnect was requested')
    await expect(alert).not.toContainText('unsafe disconnect detail')
    await alert.getByRole('button', { name: 'Retry revoke' }).click()
    await expect.poll(() => revokeAttempts).toBe(2)
    await expect(page.getByText('Token revoked')).toBeVisible()
  })

  test('cancels revocation, accepts disconnected zero, and renders generic errors safely', async ({
    loggedInPage: page,
  }) => {
    await prepareAdmin(page)
    let tokens = [activeToken]
    await mockTokenList(page, () => tokens)
    let revokeAttempts = 0
    await page.route('**/api/v1/opamp/tokens/*/revoke', async (route) => {
      revokeAttempts += 1
      if (revokeAttempts === 1) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'unsafe revoke body' }),
        })
        return
      }
      const repeatedSuccessToken = { ...activeToken, status: 'revoked' as const }
      tokens = [repeatedSuccessToken]
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ token: repeatedSuccessToken, disconnected_connections: 0 }),
      })
    })

    await page.goto('/admin/opamp/tokens')
    const row = page.getByTestId(`opamp-token-row-${activeToken.id}`)
    await row.getByRole('button', { name: 'Revoke' }).click()
    await page.getByRole('button', { name: 'Cancel' }).click()
    expect(revokeAttempts).toBe(0)

    await row.getByRole('button', { name: 'Revoke' }).click()
    await page.getByRole('button', { name: 'Confirm revoke' }).click()
    const alert = page.getByRole('alert')
    await expect(alert).toContainText('Unable to revoke the token')
    await expect(alert).not.toContainText('unsafe revoke body')
    await alert.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByText('Token revoked')).toBeVisible()
    await expect.poll(() => revokeAttempts).toBe(2)
  })
})

function publicIdFromRevokeRoute(route: Route): string {
  const pathParts = new URL(route.request().url()).pathname.split('/')
  return decodeURIComponent(pathParts.at(-2) ?? '')
}
