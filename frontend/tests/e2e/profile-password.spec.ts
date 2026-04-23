import { test, expect, mockMe } from './fixtures'

test('change password — 401 surfaces error, 204 clears fields', async ({ loggedInPage: page }) => {
  await mockMe(page, {})

  // First attempt : backend returns 400 with a descriptive error.
  // Note: 401 would trigger the axios interceptor redirect to /login, so we use
  // 400 instead — Profile.tsx maps 400 to err.response.data.error if present.
  await page.route('**/api/me/password', (route) => route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: 'Current password is incorrect.' }) }))

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
