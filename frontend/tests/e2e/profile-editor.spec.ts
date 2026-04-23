import { test, expect, mockMe } from './fixtures'

test('editor sees profile with editor chip, no Administration', async ({ loggedInPage: page }) => {
  await mockMe(page, {
    groups: [{ id: 'grp_system_editor', name: 'editor', role: 'editor', is_system: true, created_at: new Date().toISOString() }],
  })
  await page.goto('/profile')
  await expect(page.getByRole('heading', { name: /profile|profil/i })).toBeVisible()
  // Scope to main content to avoid matching the sidebar identity card.
  await expect(page.getByRole('main').getByText(/editor/i)).toBeVisible()
  await expect(page.getByRole('link', { name: /administration/i })).toHaveCount(0)
})
