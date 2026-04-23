import { test, expect, type Page } from '@playwright/test'

const ADMIN = {
  email: 'admin@e2e.local',
  initialPassword: 'initialPass!!!12',
  newPassword: 'changedPass!!!12',
}

// Login helper. Uses explicit IDs from Login.tsx to avoid label ambiguity.
async function login(page: Page, email: string, password: string) {
  const methodsReady = page.waitForResponse(
    (r) => r.url().includes('/api/auth/methods') && r.status() === 200,
    { timeout: 15_000 },
  )
  await page.goto('/login')
  await methodsReady

  await page.locator('#login-email').fill(email)
  await page.locator('#login-password').fill(password)

  // After submit, AppShell re-runs its boot effect with the token now in
  // localStorage and calls GET /api/me. Wait for that to land before returning
  // so `me` is hydrated in the store.
  const meReady = page.waitForResponse(
    (r) => r.url().includes('/api/me') && !r.url().includes('/api/me/') && r.status() === 200,
    { timeout: 15_000 },
  )
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.waitForURL('/', { timeout: 15_000 })
  await meReady
}

test.describe.serial('Profile flow against real backend', () => {
  test('login → change password → re-login with new password → theme + language persistence → logout', async ({ page }) => {

    // ── 1. Login with seeded credentials ────────────────────────────────────
    await login(page, ADMIN.email, ADMIN.initialPassword)

    // ── 2. Profile page shows email + administrator group chip ───────────────
    await page.getByRole('link', { name: 'Profile' }).click()
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
    // The email appears both in the IdentityCard and in the profile form;
    // scope to main to avoid strict mode ambiguity.
    await expect(page.getByRole('main').getByText(ADMIN.email)).toBeVisible()
    // The group chip renders the group name; seeded admin belongs to 'administrator'.
    await expect(page.locator('.chip-row .chip').filter({ hasText: 'administrator' })).toBeVisible()

    // ── 3. Change password ───────────────────────────────────────────────────
    // Use explicit IDs from Profile.tsx to avoid matching the login fields.
    await page.locator('#pw-current').fill(ADMIN.initialPassword)
    await page.locator('#pw-new').fill(ADMIN.newPassword)
    await page.locator('#pw-confirm').fill(ADMIN.newPassword)
    await page.getByRole('button', { name: 'Change password' }).click()
    // Success message from profile.security.success = "Password updated."
    await expect(page.getByText('Password updated.')).toBeVisible({ timeout: 5_000 })

    // ── 4. Logout via IdentityCard popover ───────────────────────────────────
    await page.locator('.identity-trigger').click()
    // account.logout = "Sign out"
    await page.locator('.identity-popover').getByRole('button', { name: 'Sign out' }).click()
    await page.waitForURL('/login')

    // ── 5. Re-login with the NEW password (proves DB persistence) ────────────
    await login(page, ADMIN.email, ADMIN.newPassword)

    // ── 6. Toggle theme to Dark, verify data-theme on <html> ─────────────────
    await page.getByRole('link', { name: 'Profile' }).click()
    // Radio label text from profile.preferences.theme_dark = "Dark"
    await page.getByRole('radio', { name: 'Dark' }).click()
    // useTheme hook maps 'dark' preference → data-theme="signal"
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'signal', { timeout: 5_000 })

    // ── 7. Reload — theme preference persists ────────────────────────────────
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'signal', { timeout: 5_000 })

    // ── 8. Switch language to French ─────────────────────────────────────────
    await page.locator('#lang-select').selectOption('fr')
    // profile.title in fr = "Profil"
    await expect(page.getByRole('heading', { name: 'Profil' })).toBeVisible({ timeout: 5_000 })

    // ── 9. Reload — language preference persists ─────────────────────────────
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Profil' })).toBeVisible({ timeout: 5_000 })

    // ── 10. Reset preferences to known state (theme=system, language=en) ─────
    // Keeps future manual runs consistent; volume is wiped by the script anyway.
    await page.locator('#lang-select').selectOption('en')
    await page.getByRole('radio', { name: 'System' }).click()
  })
})
