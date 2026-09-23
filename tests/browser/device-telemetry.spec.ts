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

  test('shows populated history as stacked cards at 390px without page overflow', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 390, height: 844 })
    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'mobile'))
    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    await publishSimulatorTelemetry(deviceID, 23.5)
    await waitForTemperature(page, deviceID, 23.5)
    await publishSimulatorTelemetry(deviceID, 28)
    await waitForTemperature(page, deviceID, 28)
    await page.getByRole('button', { name: 'Refresh telemetry' }).click()

    await expect(page.getByRole('heading', { name: '28°C' })).toBeVisible()
    await expect(page.getByRole('list', { name: 'Recent telemetry observations, newest first' })).toBeVisible()
    await expect(page.locator('.telemetry-history-cards > li')).toHaveCount(2)
    await expect(page.locator('.telemetry-history-cards > li').first()).toContainText('28°C')
    await expect(page.locator('.telemetry-history-cards > li').nth(1)).toContainText('23.5°C')
    await expect(page.locator('.telemetry-table-wrap')).toBeHidden()

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth)
  })

  test('shows populated history as stacked cards at 320px without page overflow', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 320, height: 844 })
    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'narrow'))
    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    await publishSimulatorTelemetry(deviceID, 18)
    await waitForTemperature(page, deviceID, 18)
    await publishSimulatorTelemetry(deviceID, 26.5)
    await waitForTemperature(page, deviceID, 26.5)
    await page.getByRole('button', { name: 'Refresh telemetry' }).click()

    await expect(page.getByRole('heading', { name: '26.5°C' })).toBeVisible()
    await expect(page.getByRole('list', { name: 'Recent telemetry observations, newest first' })).toBeVisible()
    await expect(page.locator('.telemetry-history-cards > li')).toHaveCount(2)
    await expect(page.locator('.telemetry-history-cards > li').first()).toContainText('26.5°C')
    await expect(page.locator('.telemetry-history-cards > li').nth(1)).toContainText('18°C')
    await expect(page.locator('.telemetry-table-wrap')).toBeHidden()

    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth)
  })

  test('hides colliding time labels on a narrow chart with dense history', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 320, height: 844 })
    const deviceID = await seedDevice(page, uniqueKey(testInfo, 'dense-chart'))
    const firstObservedAt = Date.UTC(2026, 8, 1)
    const observations = Array.from({ length: 50 }, (_, index) => {
      const observedAt = new Date(firstObservedAt + (49 - index) * 30 * 60 * 1000).toISOString()
      return {
        messageId: `00000000-0000-4000-8000-${index.toString(16).padStart(12, '0')}`,
        observedAt,
        receivedAt: new Date(Date.parse(observedAt) + 1000).toISOString(),
        temperatureCelsius: 18 + index / 10,
      }
    })

    await page.route('**/api/graphql', async (route) => {
      const request = route.request()
      if (request.postDataJSON().query.includes('query DeviceTelemetryOverview')) {
        const current = observations[0]
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: {
              deviceCurrentState: { ...current, lastSeenAt: current.observedAt },
              deviceTelemetry: {
                edges: observations.map((node, index) => ({ cursor: `cursor-${index}`, node })),
                pageInfo: { endCursor: null, hasNextPage: false },
              },
            },
          }),
        })
        return
      }
      await route.continue()
    })

    await page.goto(`/devices/${deviceID}`)
    await expect(page.getByRole('img', { name: 'Temperature trend for 50 observations.' })).toBeVisible()
    const chart = page.locator('.telemetry-chart')
    await chart.scrollIntoViewIfNeeded()
    await expect(chart.locator('svg')).toBeVisible()
    await expect.poll(async () => chart.locator('svg text').count()).toBeGreaterThan(1)

    const timeLabels = await chart.locator('svg text').evaluateAll((elements) => {
      const chart = document.querySelector('.telemetry-chart')
      if (!chart) return []
      const chartRect = chart.getBoundingClientRect()
      return elements.map((element) => {
        const rect = element.getBoundingClientRect()
        return {
          text: element.textContent?.trim() ?? '',
          left: rect.left,
          right: rect.right,
          top: rect.top,
          visible: getComputedStyle(element).display !== 'none' && getComputedStyle(element).visibility !== 'hidden',
        }
      }).filter(label => label.visible && label.text !== '' && label.right > label.left
        && label.text.includes(':') && label.top > chartRect.top + chartRect.height * 0.68)
    })

    expect(timeLabels.length).toBeGreaterThan(1)
    const orderedLabels = timeLabels.toSorted((left, right) => left.left - right.left)
    for (let index = 1; index < orderedLabels.length; index += 1) {
      expect(orderedLabels[index].left).toBeGreaterThanOrEqual(orderedLabels[index - 1].right)
    }
  })
})
