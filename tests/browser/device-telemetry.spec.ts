import { expect, publishSimulatorTelemetry, test } from './fixtures'

const telemetryQuery = `query DeviceTelemetry($deviceID: ID!) {
  deviceCurrentState(deviceId: $deviceID) {
    temperatureCelsius
    lastSeenAt
  }
}`

function uniqueKey(testInfo: { workerIndex: number; retry: number }, suffix: string) {
  return `browser-telemetry-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}-${suffix}`
}

async function seedDevice(page: import('@playwright/test').Page, deviceKey: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: `mutation CreateDevice($input: CreateDeviceInput!) {
        createDevice(input: $input) { id }
      }`,
      variables: { input: { deviceKey, displayName: 'Browser telemetry device' } },
    },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data.createDevice.id as string
}

async function readCurrentState(page: import('@playwright/test').Page, deviceID: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: { query: telemetryQuery, variables: { deviceID } },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data.deviceCurrentState as { temperatureCelsius: number, lastSeenAt: string } | null
}

async function waitForTemperature(page: import('@playwright/test').Page, deviceID: string, temperature: number) {
  await expect.poll(async () => (await readCurrentState(page, deviceID))?.temperatureCelsius, {
    timeout: 10_000,
  }).toBe(temperature)
}

test.describe('telemetry console journey', () => {
  test('shows empty state, committed simulator telemetry, refresh, chart, and stale signal', async ({ page }, testInfo) => {
    const consoleErrors: string[] = []
    const pageErrors: string[] = []
    page.on('console', message => {
      if (message.type() === 'error') consoleErrors.push(message.text())
    })
    page.on('pageerror', error => pageErrors.push(error.message))
    await page.clock.install({ time: Date.now() })

    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'journey'))
    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    await publishSimulatorTelemetry(deviceID, 23.5)
    await waitForTemperature(page, deviceID, 23.5)
    await page.getByRole('button', { name: 'Refresh telemetry' }).click()
    await expect(page.getByRole('heading', { name: '23.5°C' })).toBeVisible()
    await expect(page.getByText('Recent signal')).toBeVisible()
    await expect(page.locator('.telemetry-table tbody tr')).toHaveCount(1)

    await publishSimulatorTelemetry(deviceID, 31)
    await waitForTemperature(page, deviceID, 31)
    await page.getByRole('button', { name: 'Refresh telemetry' }).click()
    await expect(page.getByRole('heading', { name: '31°C' })).toBeVisible()
    await expect(page.getByRole('img', { name: 'Temperature trend for 2 observations.' })).toBeVisible()
    await expect(page.getByText(/Loaded 2 observations/)).toBeVisible()
    await expect(page.locator('.telemetry-table tbody tr')).toHaveCount(2)

    await page.clock.fastForward(5 * 60 * 1000 + 1_000)
    await expect(page.getByText('Stale signal')).toBeVisible()
    expect(consoleErrors).toEqual([])
    expect(pageErrors).toEqual([])
  })

  test('keeps the empty telemetry page usable at mobile width', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 390, height: 844 })
    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'mobile'))
    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth)
  })

  test('keeps the telemetry page usable at narrow 320px width', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 320, height: 844 })
    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'narrow'))
    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth)
  })
})
