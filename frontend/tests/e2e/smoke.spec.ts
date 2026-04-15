import { test, expect } from '@playwright/test'

test('app loads and renders root', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveTitle(/magnify/i)
})
