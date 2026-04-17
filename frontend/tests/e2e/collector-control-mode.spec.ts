import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const SUPERVISED_ID = 'ssssssss-ssss-ssss-ssss-ssssssssssss'
const READONLY_ID   = 'rrrrrrrr-rrrr-rrrr-rrrr-rrrrrrrrrrrr'
const CONFIG_ID     = 'cfg1'

function baseAgent(id: string, name: string, acceptsRemoteConfig: boolean) {
  return {
    id,
    display_name: name,
    type: 'collector',
    version: '0.98.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    active_config_id: CONFIG_ID,
    accepts_remote_config: acceptsRemoteConfig,
    available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
  }
}

function mockList(page: Page) {
  return page.route('**/api/agents', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        baseAgent(SUPERVISED_ID, 'collector-supervised', true),
        baseAgent(READONLY_ID,   'collector-readonly',   false),
      ]),
    }),
  )
}

function mockAgent(page: Page, id: string, acceptsRemoteConfig: boolean) {
  const name = id === SUPERVISED_ID ? 'collector-supervised' : 'collector-readonly'
  return page.route(`**/api/agents/${id}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(baseAgent(id, name, acceptsRemoteConfig)),
    }),
  )
}

function mockConfig(page: Page) {
  return page.route(`**/api/configs/${CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: CONFIG_ID,
        name: 'current',
        content: 'receivers:\n  otlp: {}\n',
        created_at: new Date().toISOString(),
        created_by: 'agent-reported',
      }),
    }),
  )
}

function mockHistory(page: Page, id: string) {
  return page.route(`**/api/agents/${id}/configs`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

function mockAlerts(page: Page) {
  return page.route('**/api/alerts**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

test('Inventory shows supervised pill only on supervised collectors', async ({ loggedInPage: page }) => {
  await mockList(page)
  await page.goto('/inventory')

  const supervisedCard = page.locator('.agent-card', { hasText: 'collector-supervised' })
  const readonlyCard   = page.locator('.agent-card', { hasText: 'collector-readonly' })

  await expect(supervisedCard.locator('.agent-supervised-pill')).toBeVisible()
  await expect(readonlyCard.locator('.agent-supervised-pill')).toHaveCount(0)
})

test('Inventory control filter narrows to supervised or read-only', async ({ loggedInPage: page }) => {
  await mockList(page)

  await page.goto('/inventory?control=supervised')
  await expect(page.locator('.agent-card')).toHaveCount(1)
  await expect(page.locator('.agent-card')).toContainText('collector-supervised')

  await page.goto('/inventory?control=readonly')
  await expect(page.locator('.agent-card')).toHaveCount(1)
  await expect(page.locator('.agent-card')).toContainText('collector-readonly')
})

test('Read-only collector detail page hides Edit and shows note', async ({ loggedInPage: page }) => {
  await mockAgent(page, READONLY_ID, false)
  await mockConfig(page)
  await mockHistory(page, READONLY_ID)

  await page.goto(`/inventory/${READONLY_ID}`)

  await expect(page.locator('.detail-cell-label', { hasText: 'Control' })).toBeVisible()
  await expect(page.locator('.detail-cell-value', { hasText: 'Read-only' })).toBeVisible()
  await expect(page.locator('.config-readonly-note')).toContainText('OpAMP Supervisor')
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push a config' })).toHaveCount(0)
})

test('Supervised collector detail page shows Edit button and Supervised cell', async ({ loggedInPage: page }) => {
  await mockAgent(page, SUPERVISED_ID, true)
  await mockConfig(page)
  await mockHistory(page, SUPERVISED_ID)

  await page.goto(`/inventory/${SUPERVISED_ID}`)

  await expect(page.locator('.detail-cell-value', { hasText: 'Supervised' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
  await expect(page.locator('.config-readonly-note')).toHaveCount(0)
})

test('Inventory control filter excludes SDK agents entirely', async ({ loggedInPage: page }) => {
  const SDK_ID = 'dddddddd-dddd-dddd-dddd-dddddddddddd'
  await page.route('**/api/agents', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        baseAgent(SUPERVISED_ID, 'collector-supervised', true),
        baseAgent(READONLY_ID,   'collector-readonly',   false),
        {
          id: SDK_ID,
          display_name: 'sdk-service',
          type: 'sdk',
          version: '1.0.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
        },
      ]),
    }),
  )

  // Baseline: all three visible with no control filter
  await page.goto('/inventory')
  await expect(page.locator('.agent-card')).toHaveCount(3)

  // control=supervised → SDK excluded, only supervised collector shown
  await page.goto('/inventory?control=supervised')
  await expect(page.locator('.agent-card')).toHaveCount(1)
  await expect(page.locator('.agent-card')).toContainText('collector-supervised')
  await expect(page.locator('.agent-card', { hasText: 'sdk-service' })).toHaveCount(0)

  // control=readonly → SDK also excluded, only read-only collector shown
  await page.goto('/inventory?control=readonly')
  await expect(page.locator('.agent-card')).toHaveCount(1)
  await expect(page.locator('.agent-card')).toContainText('collector-readonly')
  await expect(page.locator('.agent-card', { hasText: 'sdk-service' })).toHaveCount(0)
})

test('Dashboard Supervised stat card links to filtered Inventory', async ({ loggedInPage: page }) => {
  await mockList(page)
  await mockAlerts(page)

  await page.goto('/')
  const supervisedCard = page.locator('.stat-card', { hasText: 'Supervised' })
  await expect(supervisedCard.locator('.stat-value')).toHaveText('1')

  await supervisedCard.click()
  await expect(page).toHaveURL(/\/inventory\?control=supervised/)
})
