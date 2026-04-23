import { test, expect, mockMe } from './fixtures'

test('switching to fr retranslates the UI', async ({ loggedInPage: page }) => {
  const me = await mockMe(page, {})
  await page.route('**/api/me/preferences', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...me.preferences, theme: 'system', language: 'fr' }),
    })
  })

  await page.goto('/profile')
  await page.getByLabel(/language|langue/i).selectOption('fr')

  await expect(page.getByRole('heading', { name: 'Profil' })).toBeVisible()
})
