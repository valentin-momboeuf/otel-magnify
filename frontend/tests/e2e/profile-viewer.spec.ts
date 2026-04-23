import { test, expect, mockMe } from './fixtures'

test('viewer sees profile, no Administration link', async ({ loggedInPage: page }) => {
  await mockMe(page, {
    groups: [{ id: 'grp_system_viewer', name: 'viewer', role: 'viewer', is_system: true, created_at: new Date().toISOString() }],
  })
  await page.goto('/profile')
  await expect(page.getByRole('heading', { name: /profile|profil/i })).toBeVisible()
  await expect(page.getByRole('link', { name: /administration/i })).toHaveCount(0)
  // Scope to main content to avoid matching the sidebar identity card.
  await expect(page.getByRole('main').getByText(/viewer/i)).toBeVisible()
})
