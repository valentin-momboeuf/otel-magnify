import { test, expect } from './fixtures'

const mockWorkloads = [
  { id: 'w1', fingerprint_source: 'k8s', fingerprint_keys: {}, display_name: 'otel-prod',    type: 'collector', version: '0.100.0', status: 'connected',    last_seen_at: new Date().toISOString(), labels: { env: 'prod' }, accepts_remote_config: true },
  { id: 'w2', fingerprint_source: 'k8s', fingerprint_keys: {}, display_name: 'otel-staging', type: 'collector', version: '0.99.0',  status: 'degraded',     last_seen_at: new Date().toISOString(), labels: { env: 'staging' }, accepts_remote_config: false },
  { id: 'w3', fingerprint_source: 'uid', fingerprint_keys: {}, display_name: 'checkout-api', type: 'sdk',       version: '1.2.0',   status: 'disconnected', last_seen_at: new Date().toISOString(), labels: {} },
]

test.describe('Inventory redesign', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', (route) => {
      const url = route.request().url()
      if (/\/api\/workloads(\?|$)/.test(url) || url.endsWith('/api/workloads')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(mockWorkloads) })
      }
      return route.continue()
    })
  })

  test('lists all workloads with the supervised pill when applicable', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await expect(page.locator('.workload-card')).toHaveCount(3)
    await expect(page.locator('.agent-supervised-pill')).toHaveCount(1)
  })

  test('search input filters by display name', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await page.getByPlaceholder(/Search workloads|Rechercher/).fill('staging')
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('otel-staging')
  })

  test('type filter restricts results', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await page.locator('.filter-select').first().selectOption('sdk')
    await expect(page.locator('.workload-card')).toHaveCount(1)
    await expect(page.locator('.workload-card')).toContainText('checkout-api')
  })

  test('empty filter shows empty state', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await page.getByPlaceholder(/Search workloads|Rechercher/).fill('no-match-whatsoever')
    await expect(page.locator('.empty-state')).toBeVisible()
  })

  test('label chips render workload labels', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await expect(page.locator('.workload-card', { hasText: 'otel-prod' }).locator('.label-chip')).toContainText('env')
  })
})
