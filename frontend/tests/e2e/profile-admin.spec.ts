import { test, expect, mockMe } from './fixtures'

test('administrator sees Administration link and stub page', async ({ loggedInPage: page }) => {
  await mockMe(page, {
    groups: [{ id: 'grp_system_administrator', name: 'administrator', role: 'administrator', is_system: true, created_at: new Date().toISOString() }],
  })
  await page.goto('/')
  await expect(page.getByRole('link', { name: /administration/i })).toBeVisible()
  await page.getByRole('link', { name: /administration/i }).click()
  await expect(page.getByRole('heading', { name: /administration/i })).toBeVisible()
  await expect(page.getByText(/v0\.3|coming|arrive/i)).toBeVisible()
})
