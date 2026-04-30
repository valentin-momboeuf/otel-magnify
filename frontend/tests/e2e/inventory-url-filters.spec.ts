import { test, expect } from './fixtures'

const mockWorkloads = [
  {
    id: 'w-sup-1',
    fingerprint_source: 'k8s',
    fingerprint_keys: {},
    display_name: 'collector-supervised-eu',
    type: 'collector',
    version: '0.100.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    accepts_remote_config: true,
  },
  {
    id: 'w-ro-1',
    fingerprint_source: 'k8s',
    fingerprint_keys: {},
    display_name: 'collector-readonly-fr',
    type: 'collector',
    version: '0.100.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    accepts_remote_config: false,
  },
  {
    id: 'w-sdk-1',
    fingerprint_source: 'uid',
    fingerprint_keys: {},
    display_name: 'checkout-api',
    type: 'sdk',
    version: '1.2.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
  },
]

test.describe('Inventory URL-driven filters', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', (route) => {
      const url = route.request().url()
      if (/\/api\/workloads(\?|$)/.test(url) || url.endsWith('/api/workloads')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockWorkloads),
        })
      }
      return route.continue()
    })
  })

  test('deep link with ?type=collector&control=supervised filters to supervised collectors', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/inventory?type=collector&control=supervised')
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('collector-supervised-eu')

    // Dropdowns reflect the URL state.
    const typeSelect = page.locator('.filter-select').nth(0)
    const controlSelect = page.locator('.filter-select').nth(2)
    await expect(typeSelect).toHaveValue('collector')
    await expect(controlSelect).toHaveValue('supervised')
  })

  test('changing filter via URL navigation updates the list (back/forward)', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/inventory?type=collector&control=supervised')
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('collector-supervised-eu')

    await page.goto('/inventory?type=collector&control=readonly')
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('collector-readonly-fr')

    await page.goBack()
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('collector-supervised-eu')

    await page.goForward()
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('collector-readonly-fr')
  })

  test('selecting a filter dropdown writes back to the URL', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await expect(page.locator('.workload-card')).toHaveCount(3)

    await page.locator('.filter-select').nth(0).selectOption('sdk')
    await expect(page).toHaveURL(/[?&]type=sdk(?:&|$)/)
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('checkout-api')

    // Selecting "all" clears the URL parameter rather than leaving an empty value.
    await page.locator('.filter-select').nth(0).selectOption('')
    await expect(page).not.toHaveURL(/[?&]type=/)
    await expect(page.locator('.workload-card')).toHaveCount(3)
  })
})
