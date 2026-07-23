import { randomUUID } from 'node:crypto'

import { expect, test } from '@playwright/test'

test('creates, clears, reloads metadata, and revokes an OpAMP token', async ({ page }) => {
  const tokenName = `e2e-opamp-${randomUUID()}`

  await page.goto('/login')
  await page.locator('#login-email').fill('admin@e2e.local')
  await page.locator('#login-password').fill('initialPass!!!12')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.waitForURL(/\/(?:inventory)?$/, { timeout: 10_000 })

  await page.goto('/admin/opamp/tokens')
  await page.getByRole('button', { name: 'Create token' }).click()
  await page.getByLabel('Name').fill(tokenName)
  await page.getByLabel('Description').fill('Real PostgreSQL one-shot lifecycle proof')
  await page.getByLabel('Team').fill('observability')
  await page.getByLabel('Environment').fill('e2e')
  await page.getByRole('button', { name: 'Create', exact: true }).click()

  const credentialHasCanonicalFormat = await page
    .getByLabel('One-shot token value')
    .evaluate((element) =>
      /^ompt_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}$/.test(
        (element as HTMLInputElement).value,
      ),
    )
  expect(credentialHasCanonicalFormat).toBe(true)

  const browserStorageIsCredentialFree = await page.evaluate(() => {
    const credentialPattern =
      /ompt_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.[A-Za-z0-9_-]{43}/
    return [localStorage, sessionStorage].every((storage) =>
      Array.from({ length: storage.length }, (_, index) => {
        const key = storage.key(index) ?? ''
        return !credentialPattern.test(key) && !credentialPattern.test(storage.getItem(key) ?? '')
      }).every(Boolean),
    )
  })
  expect(browserStorageIsCredentialFree).toBe(true)

  await page.getByRole('button', { name: 'I have saved this token' }).click()
  await expect(page.getByLabel('One-shot token value')).toHaveCount(0)

  await page.reload()
  await expect(page.getByLabel('One-shot token value')).toHaveCount(0)
  const row = page.locator('tr').filter({ hasText: tokenName })
  await expect(row).toBeVisible()
  await expect(row.getByText(tokenName, { exact: true })).toBeVisible()

  await row.getByRole('button', { name: 'Revoke' }).click()
  await page.getByRole('button', { name: 'Confirm revoke' }).click()
  await expect(page.getByText('Token revoked')).toBeVisible()
  await page.getByRole('button', { name: 'Done' }).click()
  await expect(
    page.locator('tr').filter({ hasText: tokenName }).getByText('revoked', { exact: true }),
  ).toBeVisible()
})
