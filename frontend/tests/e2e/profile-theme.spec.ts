import { test, expect, mockMe } from './fixtures'

test('theme selection persists data-theme on <html>', async ({ loggedInPage: page }) => {
  const me = await mockMe(page, {})
  await page.route('**/api/me/preferences', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...me.preferences, theme: 'dark', language: 'en' }),
    })
  })

  await page.goto('/profile')
  await page.getByRole('radio', { name: /dark|sombre/i }).click()

  // Hook maps dark → signal.
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'signal')
})
