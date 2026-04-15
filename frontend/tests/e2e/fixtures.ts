import { test as base, expect, type Page } from '@playwright/test'

// A Playwright fixture that returns a page already authenticated with a fake
// JWT stored in localStorage. We skip the login round-trip to keep tests
// focused on the UI flows; the token payload is never verified on the
// frontend (the backend would reject it — tests rely on route mocking).
export const test = base.extend<{ loggedInPage: Page }>({
  loggedInPage: async ({ page }, use) => {
    await page.addInitScript(() => {
      localStorage.setItem('token', 'test.token.stub')
    })
    await use(page)
  },
})

export { expect }
