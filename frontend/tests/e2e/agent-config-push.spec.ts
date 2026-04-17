import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const AGENT_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'abc123'

function mockAgent(page: Page, overrides: Record<string, unknown> = {}) {
  return page.route(`**/api/agents/${AGENT_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: AGENT_ID,
        display_name: 'test-collector',
        type: 'collector',
        version: '0.98.0',
        status: 'connected',
        last_seen_at: new Date().toISOString(),
        labels: {},
        active_config_id: ACTIVE_CONFIG_ID,
        accepts_remote_config: true,
        available_components: {
          components: {
            receivers: ['otlp'],
            exporters: ['logging', 'debug'],
          },
        },
        ...overrides,
      }),
    }),
  )
}

function mockConfig(page: Page, content: string) {
  return page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content,
        created_at: new Date().toISOString(),
        created_by: 'test',
      }),
    }),
  )
}

function mockHistory(page: Page, rows: unknown[]) {
  return page.route(`**/api/agents/${AGENT_ID}/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    }),
  )
}

function mockValidate(page: Page, result: { valid: boolean; errors?: unknown[] }) {
  return page.route(`**/api/agents/${AGENT_ID}/config/validate`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(result),
    }),
  )
}

test('edit button enables YAML editing (regression)', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/inventory/${AGENT_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()

  // The draft editor is the second `.cm-content` after Edit is clicked? Actually
  // when entering edit mode, only the draft editor remains (readOnly one unmounts).
  const editor = page.locator('.cm-content').first()
  await editor.click()
  await page.keyboard.type('# edited')
  await expect(editor).toContainText('# edited')
})

test('validate exposes errors and blocks push', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: false,
    errors: [{ code: 'undefined_component', message: 'pipeline "traces" references exporter "nope"', path: 'service.pipelines.traces.exporters[0]' }],
  })

  await page.goto(`/inventory/${AGENT_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('bad: yaml')
  await page.getByRole('button', { name: 'Validate' }).click()

  await expect(page.locator('.validation-errors')).toContainText('undefined_component')
  // Push stays disabled
  await expect(page.getByRole('button', { name: 'Push' })).toBeDisabled()
})

test('valid config unlocks push button', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })

  await page.goto(`/inventory/${AGENT_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # touched')
  await page.getByRole('button', { name: 'Validate' }).click()

  await expect(page.locator('.validation-ok')).toContainText('valid')
  await expect(page.getByRole('button', { name: 'Push' })).toBeEnabled()
})

test('push failed shows error banner and preserves draft', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await page.route(`**/api/agents/${AGENT_ID}/config`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'deadbeefdeadbeef' }),
    }),
  )

  await page.goto(`/inventory/${AGENT_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type(' # touched')
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.locator('.validation-ok')).toBeVisible()
  await page.getByRole('button', { name: 'Push' }).click()

  // Simulate FAILED WS event
  await page.evaluate(() => {
    const evt = {
      type: 'agent_config_status',
      agent_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      status: {
        status: 'failed',
        config_hash: 'deadbeefdeadbeef',
        error_message: "unknown exporter 'othttp'",
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  })

  await expect(page.locator('.push-banner-failed')).toContainText("unknown exporter 'othttp'")
  // Draft preserved — editor still shows our addition
  await expect(page.locator('.cm-content').first()).toContainText('# touched')
})

test('diff tab shows two editor panels', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])

  await page.goto(`/inventory/${AGENT_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('a: 2\n')
  await page.getByRole('button', { name: 'Diff' }).click()

  await expect(page.locator('.cm-mergeView .cm-editor')).toHaveCount(2)
})

test('history refreshes when WS agent_config_status arrives from another session', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'a: 1\n')

  let call = 0
  await page.route(`**/api/agents/${AGENT_ID}/configs`, (route) => {
    call += 1
    const rows = call === 1 ? [] : [{
      agent_id: AGENT_ID,
      config_id: 'ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
      applied_at: new Date().toISOString(),
      status: 'applied',
      pushed_by: 'other@user',
      error_message: '',
    }]
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    })
  })

  await page.goto(`/inventory/${AGENT_ID}`)
  // No history yet: table not rendered
  await expect(page.locator('.history-table')).toHaveCount(0)

  // Simulate a config applied event from another session (not our local push)
  await page.evaluate((agentId) => {
    const evt = {
      type: 'agent_config_status',
      agent_id: agentId,
      status: {
        status: 'applied',
        config_hash: 'cccccccc',
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  }, AGENT_ID)

  // Table appears because the query was invalidated and refetched
  await expect(page.locator('.history-table tbody tr')).toHaveCount(1)
  await expect(page.locator('.history-table')).toContainText('other@user')
})

test('history table renders with rollback action', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [
    { agent_id: AGENT_ID, config_id: '1111111111111111', applied_at: new Date().toISOString(), status: 'applied', pushed_by: 'me@x', content: 'old: true' },
    { agent_id: AGENT_ID, config_id: '2222222222222222', applied_at: new Date().toISOString(), status: 'failed', error_message: 'boom', pushed_by: 'me@x', content: 'bad' },
  ])

  await page.goto(`/inventory/${AGENT_ID}`)
  await expect(page.locator('.history-table tbody tr')).toHaveCount(2)
  await expect(page.locator('.history-error').first()).toHaveText('')
  await expect(page.locator('.history-error').nth(1)).toContainText('boom')
  await expect(page.getByRole('button', { name: 'Rollback to this' })).toBeVisible()
})

test('YAML keys are colored via Signal Deck theme', async ({ loggedInPage: page }) => {
  await mockAgent(page)
  await mockConfig(page, 'key: value\n')
  await mockHistory(page, [])

  await page.goto(`/inventory/${AGENT_ID}`)
  // Wait for the editor to render
  await expect(page.locator('.cm-content')).toBeVisible()

  // Find the span that carries the color attribution (Lezer highlight uses
  // generated class names, so we match on computed style instead of class).
  const firstSpan = page.locator('.cm-line span').first()
  const color = await firstSpan.evaluate((el) => getComputedStyle(el).color)
  expect(color).toBe('rgb(212, 168, 74)')
})
