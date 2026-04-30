import { test, expect } from '@playwright/test'
import { spawn, type ChildProcess } from 'node:child_process'
import path from 'node:path'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'

const __dirname = fileURLToPath(new URL('.', import.meta.url))

// Path resolution: defaults to ../../../../otel-magnify-enterprise/bin/server-ee
// (sibling-repo layout). Override via EE_BINARY env var if your checkout differs.
const EE_BINARY =
  process.env.EE_BINARY ??
  path.resolve(__dirname, '../../../../otel-magnify-enterprise/bin/server-ee')

const SKIP_REASON = (() => {
  if (!fs.existsSync(EE_BINARY)) {
    return `EE binary not found at ${EE_BINARY}`
  }
  // Verify the binary is actually executable on the current platform.
  // A Linux ELF binary exists on disk with X bits but cannot be spawned on
  // macOS — detect by checking the ELF magic bytes (\x7fELF) on non-Linux hosts.
  try {
    fs.accessSync(EE_BINARY, fs.constants.X_OK)
  } catch {
    return `EE binary not executable at ${EE_BINARY} (permissions)`
  }
  if (process.platform !== 'linux') {
    const magic = Buffer.alloc(4)
    const fd = fs.openSync(EE_BINARY, 'r')
    fs.readSync(fd, magic, 0, 4, 0)
    fs.closeSync(fd)
    if (magic.toString('ascii', 1, 4) === 'ELF') {
      return `EE binary is a Linux ELF but platform is ${process.platform} — build a native binary or run inside Docker`
    }
  }
  // The EE Dockerfile / standard build does not yet embed the SPA assets.
  // Without a built frontend, /login renders the placeholder index.html and
  // Playwright cannot drive the UI. Skip until a build chain wires the SPA
  // into the EE binary (or sets EE_BINARY to a binary built from a community
  // checkout with `pkg/frontend/dist` populated via `npm run build`).
  // Heuristic: the fixture index.html is ~193 bytes; a real built bundle
  // is several KB minimum.
  return null
})()

const SKIP = SKIP_REASON !== null

test.describe.configure({ mode: 'serial' })

let proc: ChildProcess | undefined
const PORT = 8080
const BASE_URL = `http://localhost:${PORT}`

async function waitFor(url: string, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const r = await fetch(url)
      if (r.ok) return
    } catch {
      // not ready yet
    }
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`timeout waiting for ${url}`)
}

test.beforeAll(async () => {
  test.skip(SKIP, SKIP_REASON ?? '')
  proc = spawn(EE_BINARY, [], {
    env: {
      ...process.env,
      JWT_SECRET: 'e2e-real-sso-secret',
      SEED_ADMIN_EMAIL: 'admin@e2e-sso.local',
      SEED_ADMIN_PASSWORD: 'admin12345',
      DB_DRIVER: 'sqlite',
      DB_DSN: ':memory:',
      LISTEN_ADDR: `:${PORT}`,
      // SSO_SP_CERT_PATH/SSO_SP_KEY_PATH unset: the registry boots empty.
    },
    stdio: 'inherit',
  })
  await waitFor(`${BASE_URL}/healthz`)
})

test.afterAll(() => {
  if (proc) proc.kill('SIGTERM')
})

test('full SSO admin lifecycle: create provider → button on /login → delete', async ({ page }) => {
  // Login as seeded admin.
  await page.goto('/login')
  await page.locator('#login-email').fill('admin@e2e-sso.local')
  await page.locator('#login-password').fill('admin12345')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.waitForURL(/\/(?:inventory)?$/, { timeout: 10_000 })

  // Navigate to SSO admin.
  await page.goto('/admin/sso/providers')
  await expect(page.getByText(/no SSO provider configured/i)).toBeVisible()

  // Create a provider with inline XML metadata (URL would require a reachable IdP).
  await page.getByRole('button', { name: /\+ new provider/i }).click()
  await page.getByLabel(/Provider ID/i).fill('keycloak-real')
  await page.getByLabel(/Display name/i).fill('Keycloak Real')
  await page.getByRole('radio', { name: /Metadata XML/i }).check()
  await page
    .getByLabel(/Choose XML file/i)
    .setInputFiles(path.resolve(__dirname, '../fixtures/idp-metadata.xml'))
  await page
    .getByRole('textbox', { name: /SP entity ID/i })
    .fill(`${BASE_URL}/api/auth/sso/keycloak-real/metadata`)
  await page.getByRole('button', { name: /save/i }).click()

  // Back on the list, the provider is visible.
  await expect(page.getByText('Keycloak Real')).toBeVisible()

  // /login now advertises the SSO button.
  await page.goto('/login')
  await expect(page.getByRole('link', { name: /sign in with keycloak real/i })).toBeVisible()

  // Add a mapping on the edit page.
  await page.goto('/admin/sso/providers/keycloak-real')
  await page.getByLabel(/IdP group/i).fill('e2e-admins')
  await page.getByLabel(/System group/i).selectOption('administrator')
  await page.getByRole('button', { name: /add mapping/i }).click()
  await expect(page.getByText('e2e-admins')).toBeVisible()

  // Delete the provider; confirm dialog.
  page.on('dialog', (d) => d.accept())
  await page.goto('/admin/sso/providers')
  await page
    .getByRole('row', { name: /keycloak-real/i })
    .getByRole('button', { name: /delete/i })
    .click()

  // /login no longer shows the SSO button.
  await page.goto('/login')
  await expect(page.getByRole('link', { name: /sign in with keycloak real/i })).toHaveCount(0)
})
