import type { Page, TestInfo } from '@playwright/test'

import {
  createTemperatureRule,
  expect,
  publishSimulatorTelemetry,
  readAlerts,
  startCommandSimulator,
  test,
} from './fixtures'

const telemetryQuery = `query ProductLoopTelemetry($deviceId: ID!) {
  deviceCurrentState(deviceId: $deviceId) {
    messageId
    observedAt
    receivedAt
    temperatureCelsius
    lastSeenAt
  }
  deviceTelemetry(deviceId: $deviceId, first: 10) {
    edges { node { messageId observedAt receivedAt temperatureCelsius } }
    pageInfo { hasNextPage }
  }
}`

const commandsQuery = `query ProductLoopCommands($deviceId: ID!) {
  deviceCommands(deviceId: $deviceId, first: 20) {
    edges { node { id deviceId type status dispatchedAt acknowledgedAt terminalAt failureCode } }
    pageInfo { hasNextPage }
  }
}`

type TelemetryOverview = {
  deviceCurrentState: {
    messageId: string
    observedAt: string
    receivedAt: string
    temperatureCelsius: number
    lastSeenAt: string
  } | null
  deviceTelemetry: {
    edges: Array<{ node: { messageId: string, temperatureCelsius: number } }>
    pageInfo: { hasNextPage: boolean }
  }
}

async function readTelemetry(page: Page, deviceId: string): Promise<TelemetryOverview> {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: { query: telemetryQuery, variables: { deviceId } },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data as TelemetryOverview
}

async function readCommands(page: Page, deviceId: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: { query: commandsQuery, variables: { deviceId } },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data.deviceCommands.edges.map((edge: { node: Record<string, unknown> }) => edge.node) as Array<{
    id: string
    deviceId: string
    type: string
    status: string
    dispatchedAt: string | null
    acknowledgedAt: string | null
    terminalAt: string | null
    failureCode: string | null
  }>
}

function uniqueKey(testInfo: TestInfo) {
  return `mvp013-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}`
}

test.describe('MVP-013 complete product loop', () => {
  test('provisions, investigates simulator telemetry, and observes the stored PING result', async ({ page }, testInfo) => {
    test.setTimeout(60_000)
    const pageErrors: string[] = []
    const consoleErrors: string[] = []
    page.on('pageerror', error => pageErrors.push(error.message))
    page.on('console', message => {
      if (message.type() === 'error') consoleErrors.push(message.text())
    })

    const deviceKey = uniqueKey(testInfo)
    const displayName = 'MVP-013 product-loop device'
    await page.goto('/devices/new')
    await page.getByLabel('Device key').fill(deviceKey)
    await page.getByLabel('Display name').fill(displayName)
    await page.getByRole('button', { name: 'Create device' }).click()
    await expect(page).toHaveURL(/\/devices\/[0-9a-f-]{36}$/u)
    const deviceId = new URL(page.url()).pathname.split('/').at(-1)
    expect(deviceId).toMatch(/^[0-9a-f-]{36}$/u)
    await expect(page.getByRole('heading', { name: displayName })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'No telemetry yet.' })).toBeVisible()

    const rule = await createTemperatureRule(page, deviceId!)
    expect(rule).toMatchObject({ enabled: true, comparator: 'GT', thresholdCelsius: 25 })

    const belowThreshold = await publishSimulatorTelemetry(deviceId!, 23.5)
    await expect.poll(async () => (await readTelemetry(page, deviceId!)).deviceCurrentState?.messageId, {
      timeout: 10_000,
    }).toBe(belowThreshold.messageID)
    await page.getByRole('button', { name: 'Refresh telemetry' }).click()
    await expect(page.getByRole('heading', { name: '23.5°C' })).toBeVisible()
    await expect(page.locator('.telemetry-table tbody tr')).toHaveCount(1)
    await expect(page.locator('.telemetry-table tbody')).toContainText('23.5°C')
    expect((await readAlerts(page, deviceId)).edges.some(edge => edge.node.messageId === belowThreshold.messageID)).toBe(false)

    const aboveThreshold = await publishSimulatorTelemetry(deviceId!, 31)
    await expect.poll(async () => (await readTelemetry(page, deviceId!)).deviceCurrentState?.messageId, {
      timeout: 10_000,
    }).toBe(aboveThreshold.messageID)
    await expect.poll(async () => (await readAlerts(page, deviceId)).edges.filter(edge => edge.node.messageId === aboveThreshold.messageID).length, {
      timeout: 10_000,
    }).toBe(1)
    const alert = (await readAlerts(page, deviceId)).edges.find(edge => edge.node.messageId === aboveThreshold.messageID)?.node
    expect(alert).toBeDefined()
    expect(alert).toMatchObject({
      deviceId,
      ruleId: rule.id,
      messageId: aboveThreshold.messageID,
      observedAt: aboveThreshold.observedAt,
      temperatureCelsius: 31,
      comparator: 'GT',
      thresholdCelsius: 25,
    })

    await page.getByRole('button', { name: 'Refresh telemetry' }).click()
    await expect(page.getByRole('heading', { name: '31°C' })).toBeVisible()
    await expect(page.locator('.telemetry-table tbody tr')).toHaveCount(2)
    await expect(page.locator('.telemetry-table tbody')).toContainText('31°C')
    await page.getByRole('link', { name: `View alerts for ${deviceKey}` }).click()
    await expect(page).toHaveURL(url => url.pathname === '/alerts' && url.searchParams.get('deviceId') === deviceId)
    await expect(page.getByRole('heading', { name: `Alerts for ${displayName}` })).toBeVisible()
    const alertRow = page.locator('.alert-table tbody tr').filter({ hasText: deviceId! })
    await expect(alertRow).toHaveCount(1)
    await expect(alertRow).toContainText('Temperature: 31 °C is greater than 25 °C')
    await alertRow.getByRole('link', { name: 'View details' }).click()
    await expect(page).toHaveURL(url => url.pathname === `/alerts/${alert?.id}` && url.searchParams.get('deviceId') === deviceId)
    await expect(page.getByRole('heading', { name: 'Triggering context' })).toBeVisible()
    await expect(page.locator('.alert-detail-card')).toContainText(aboveThreshold.messageID)
    await expect(page.locator('.alert-detail-card')).toContainText(displayName)
    await expect(page.getByText('Observed', { exact: true })).toBeVisible()
    await expect(page.getByText('Received', { exact: true })).toBeVisible()
    await expect(page.getByText('Recorded', { exact: true })).toBeVisible()
    await page.getByRole('link', { name: displayName }).click()
    await expect(page).toHaveURL(new RegExp(`/devices/${deviceId}$`, 'u'))

    await startCommandSimulator(deviceId!, 'success')
    await page.getByRole('button', { name: 'Send PING' }).click()
    await expect(page.getByRole('heading', { name: 'Confirm diagnostic PING' })).toBeVisible()
    await page.getByRole('button', { name: 'Confirm and send PING' }).click()
    await expect(page.locator('.command-current').getByText('Completed', { exact: true })).toBeVisible({ timeout: 20_000 })
    await expect(page.locator('.command-current')).toContainText('The device reported that the diagnostic PING completed.')

    let commands = await readCommands(page, deviceId!)
    expect(commands).toHaveLength(1)
    const command = commands[0]!
    expect(command).toMatchObject({
      deviceId,
      type: 'PING',
      status: 'COMPLETED',
      failureCode: null,
    })
    expect(command.dispatchedAt).not.toBeNull()
    expect(command.acknowledgedAt).not.toBeNull()
    expect(command.terminalAt).not.toBeNull()

    await page.reload()
    await expect(page.locator('.command-current')).toContainText('Completed', { timeout: 10_000 })
    const commandRow = page.locator('.command-history-row').filter({ hasText: command.id })
    await expect(commandRow).toHaveCount(1)
    await commandRow.click()
    await expect(page.locator('.command-current')).toContainText(command.id)
    await expect(page.locator('.telemetry-table tbody tr')).toHaveCount(2)
    commands = await readCommands(page, deviceId!)
    expect(commands).toHaveLength(1)
    expect(commands[0]).toMatchObject({ id: command.id, status: 'COMPLETED' })
    expect(pageErrors).toEqual([])
    expect(consoleErrors).toEqual([])
  })
})
