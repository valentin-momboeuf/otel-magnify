import { test, expect } from './fixtures'

test.describe('Workload Instances tab', () => {
  test('renders live instances and highlights drift', async ({ loggedInPage: page }) => {
    const workloadID = 'w1'

    await page.route(`**/api/workloads/${workloadID}`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: workloadID,
          fingerprint_source: 'k8s',
          fingerprint_keys: { kind: 'deployment', name: 'otel' },
          display_name: 'otel-collector',
          type: 'collector',
          version: '0.100.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
          active_config_hash: 'abcdef1234567890',
          accepts_remote_config: true,
        }),
      })
    })

    await page.route(`**/api/workloads/${workloadID}/instances`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            instance_uid: 'uid-aaaaaaaa',
            pod_name: 'otel-abc',
            version: '0.100.0',
            connected_at: new Date().toISOString(),
            last_message_at: new Date().toISOString(),
            effective_config_hash: 'abcdef1234567890',
            healthy: true,
          },
          {
            instance_uid: 'uid-bbbbbbbb',
            pod_name: 'otel-xyz',
            version: '0.100.0',
            connected_at: new Date().toISOString(),
            last_message_at: new Date().toISOString(),
            effective_config_hash: 'deadbeefcafebabe',
            healthy: true,
          },
        ]),
      })
    })

    await page.route(`**/api/workloads/${workloadID}/events*`, async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })

    await page.goto(`/workloads/${workloadID}`)

    await page.getByRole('button', { name: /^instances$/i }).click()

    await expect(page.getByText('otel-abc')).toBeVisible()
    await expect(page.getByText('otel-xyz')).toBeVisible()
    await expect(page.locator('.instance-drift-tag')).toContainText(/drift/i)
  })
})
