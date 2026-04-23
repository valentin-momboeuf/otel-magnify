import { test, expect, type Page } from '@playwright/test'

const ADMIN = {
  email: 'admin@e2e.local',
  initialPassword: 'initialPass!!!12',
  newPassword: 'changedPass!!!12',
}

// Login helper. Uses explicit IDs from Login.tsx to avoid label ambiguity.
async function login(page: Page, email: string, password: string) {
  // AppShell mounts on every route including /login and fires meAPI.get(). Without
  // a token in localStorage the server returns 401, which the axios interceptor
  // handles by doing `window.location.href = '/login'` — a full-page hard reload
  // that restarts React, causing an infinite reload loop while on the login page.
  //
  // Workaround: intercept /api/me at the network layer so it never reaches the
  // server while on the login page. We return 200 with a dummy body so the
  // interceptor (which only triggers on 401) never fires. The mock is removed
  // once we navigate away from /login.
  await page.route('**/api/me', (route) => {
    // Abort the request silently — the catch() in AppShell swallows the error.
    route.abort()
  })

  // Similarly, the connectWS() call on mount will try to open a WebSocket.
  // Abort it at the network layer to prevent noise in the test.
  await page.route('**/ws**', (route) => route.abort())

  const methodsReady = page.waitForResponse(
    (r) => r.url().includes('/api/auth/methods') && r.status() === 200,
    { timeout: 15_000 },
  )
  await page.goto('/login')
  // Wait for getMethods to settle so the Login form has finished re-rendering.
  await methodsReady

  // Remove the intercepts — after successful login the real /api/me and /ws
  // calls must go through so the AppShell can hydrate the session.
  await page.unroute('**/api/me')
  await page.unroute('**/ws**')

  await page.locator('#login-email').fill(email)
  await page.locator('#login-password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  // Wait for the SPA to navigate to /.
  await page.waitForURL('/', { timeout: 15_000 })

  // AppShell.useEffect fires meAPI.get() once on mount. During the login page
  // phase it was aborted, so `me` is still null after the SPA navigate to `/`.
  // Force a full page reload so AppShell re-mounts with the real JWT in
  // localStorage, which fires meAPI.get() for real and hydrates `me`.
  const meReady = page.waitForResponse(
    (r) => r.url().includes('/api/me') && !r.url().includes('/api/me/') && r.status() === 200,
    { timeout: 15_000 },
  )
  await page.reload()
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
