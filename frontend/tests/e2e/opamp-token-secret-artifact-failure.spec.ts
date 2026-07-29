import { test } from '@playwright/test'

process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1'

test.use({
  trace: 'off',
  screenshot: 'off',
  video: 'off',
})

test.skip(
  process.env.OPAMP_SECRET_ARTIFACT_PROBE !== '1',
  'Runs only through scripts/test-opamp-secret-artifacts.sh',
)

test.afterEach(async ({ page }) => {
  if (page.isClosed()) return
  await page
    .evaluate(() => {
      const field = document.querySelector<HTMLInputElement>('.opamp-token-secret-value')
      if (field) field.value = ''
    })
    .catch(() => undefined)
})

test('controlled failure while a one-shot credential is visible', async ({ page }) => {
  await page.setContent(
    '<label>One-shot token value<input class="opamp-token-secret-value" readonly></label>',
  )
  await page.evaluate(() => {
    const field = document.querySelector<HTMLInputElement>('.opamp-token-secret-value')
    if (!field) throw new Error('artifact probe setup failed')

    const idBytes = crypto.getRandomValues(new Uint8Array(16))
    idBytes[6] = (idBytes[6] & 0x0f) | 0x40
    idBytes[8] = (idBytes[8] & 0x3f) | 0x80
    const idHex = Array.from(idBytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
    const id = `${idHex.slice(0, 8)}-${idHex.slice(8, 12)}-${idHex.slice(12, 16)}-${idHex.slice(16, 20)}-${idHex.slice(20)}`

    const suffixBytes = crypto.getRandomValues(new Uint8Array(32))
    let binary = ''
    for (const byte of suffixBytes) binary += String.fromCharCode(byte)
    const suffix = btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '')
    field.value = `ompt_${id}.${suffix}`
  })

  throw new Error('controlled artifact probe failure')
})
