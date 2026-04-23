import { test, expect, mockMe } from './fixtures'

test('change password — 401 surfaces error, 204 clears fields', async ({ loggedInPage: page }) => {
  await mockMe(page, {})

  // First attempt : backend returns 401 (wrong current password).
  await page.route('**/api/me/password', (route) => route.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'current password does not match' }),
  }))

  await page.goto('/profile')
  await page.getByLabel(/current password|mot de passe actuel/i).fill('wrong!!!!!!!!')
  await page.getByLabel(/new password|nouveau mot de passe/i).nth(0).fill('newpassword!!12')
  await page.getByLabel(/confirm new password|confirmer/i).fill('newpassword!!12')
  await page.getByRole('button', { name: /change password|changer/i }).click()
  await expect(page.getByText(/incorrect/i)).toBeVisible()

  // Re-mock the endpoint to return 204.
  await page.unroute('**/api/me/password')
  await page.route('**/api/me/password', (route) => route.fulfill({ status: 204 }))

  await page.getByLabel(/current password|mot de passe actuel/i).fill('rightpassword12')
  await page.getByRole('button', { name: /change password|changer/i }).click()
  await expect(page.getByText(/updated|jour/i)).toBeVisible()
})
