import { defineConfig, devices } from '@playwright/test'

const outputDir = process.env.PLAYWRIGHT_OUTPUT_DIR
if (!outputDir) {
  throw new Error('PLAYWRIGHT_OUTPUT_DIR must be supplied by scripts/e2e-real.sh')
}

export default defineConfig({
  testDir: './tests/e2e-real',
  // Serialize because tests mutate shared DB state (password change, preferences).
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  outputDir,
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // No webServer — scripts/e2e-real.sh owns the stack lifecycle.
})
