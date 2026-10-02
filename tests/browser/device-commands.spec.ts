import type { Page, TestInfo } from '@playwright/test'

import { expect, seedAlertDevice, startCommandSimulator, stopCommandSimulator, test } from './fixtures'

const deviceCommandsQuery = `query DeviceCommands($deviceId: ID!, $first: Int!, $after: String) {
  deviceCommands(deviceId: $deviceId, first: $first, after: $after) {
    edges { node { id status expiresAt } }
    pageInfo { endCursor hasNextPage }
  }
}`

function uniqueKey(testInfo: TestInfo, suffix: string) {
  return `command-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}-${suffix}`
}

async function readDeviceCommands(page: Page, deviceId: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: deviceCommandsQuery,
      variables: { deviceId, first: 20, after: null },
    },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data.deviceCommands.edges.map((edge: { node: { id: string, status: string, expiresAt: string } }) => edge.node) as Array<{
    id: string
    status: string
    expiresAt: string
  }>
}

async function createConfirmedPing(page: Page) {
  await page.getByRole('button', { name: 'Send PING' }).click()
  await expect(page.getByRole('heading', { name: 'Confirm diagnostic PING' })).toBeVisible()
  await page.getByRole('button', { name: 'Confirm and send PING' }).click()
}

test.describe('device command console journey', () => {
  test('reports the device completion and an explicit device failure', async ({ page }, testInfo) => {
    const deviceKey = `${uniqueKey(testInfo, 'outcomes')}-${'long-device-key-'.repeat(5)}`
    const device = await seedAlertDevice(page, deviceKey)
    await startCommandSimulator(device.id, 'success')
    const pageErrors: string[] = []
    const consoleErrors: string[] = []
    page.on('pageerror', error => { pageErrors.push(error.message) })
    page.on('console', message => {
      if (message.type() === 'error') consoleErrors.push(message.text())
    })
    await page.goto(`/devices/${device.id}`)
    await expect(page.getByRole('heading', { name: 'Diagnostic command' })).toBeVisible()
    const sendPing = page.getByRole('button', { name: 'Send PING' })
    await sendPing.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('heading', { name: 'Confirm diagnostic PING' })).toBeVisible()
    const cancel = page.getByRole('button', { name: 'Cancel' })
    await cancel.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('heading', { name: 'Confirm diagnostic PING' })).toHaveCount(0)
    expect(await readDeviceCommands(page, device.id)).toHaveLength(0)

    await page.getByRole('button', { name: 'Send PING' }).focus()
    await page.keyboard.press('Enter')
    const confirm = page.getByRole('button', { name: 'Confirm and send PING' })
    await confirm.focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('.command-current').getByText('Completed', { exact: true })).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('The device reported that the diagnostic PING completed.')).toBeVisible()
    const completed = await readDeviceCommands(page, device.id)
    expect(completed).toHaveLength(1)
    expect(completed[0]?.status).toBe('COMPLETED')
    await stopCommandSimulator(device.id)

    await startCommandSimulator(device.id, 'failure')
    await createConfirmedPing(page)
    await expect(page.locator('.command-current').getByText('Failed', { exact: true })).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('The device reported a failure while handling the PING.')).toBeVisible()
    const commands = await readDeviceCommands(page, device.id)
    expect(commands).toHaveLength(2)
    expect(commands.map(command => command.status)).toContain('FAILED')

    await page.setViewportSize({ width: 320, height: 844 })
    const dimensions = await page.evaluate(() => ({
      viewportWidth: document.documentElement.clientWidth,
      contentWidth: Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
    }))
    expect(dimensions.contentWidth).toBeLessThanOrEqual(dimensions.viewportWidth)
    await page.screenshot({ path: '.output/mvp012-command-console-mobile.png', fullPage: true })
    expect(pageErrors).toEqual([])
    expect(consoleErrors).toEqual([])
  })

  test('recovers a real submission after hiding its HTTP response and reloading', async ({ page }, testInfo) => {
    const device = await seedAlertDevice(page, uniqueKey(testInfo, 'lost-response'))
    await startCommandSimulator(device.id, 'success')
    let lostResponse = false
    await page.route('**/api/graphql', async (route) => {
      const request = route.request()
      if (!lostResponse && request.postDataJSON()?.query?.includes('mutation CreateCommand')) {
        await route.fetch()
        lostResponse = true
        await route.abort('failed')
        return
      }
      await route.continue()
    })

    await page.goto(`/devices/${device.id}`)
    await createConfirmedPing(page)
    await expect(page.getByRole('button', { name: 'Recover this submission' })).toBeVisible({ timeout: 15_000 })
    expect(lostResponse).toBe(true)
    await page.reload()
    await expect(page.getByRole('button', { name: 'Recover this submission' })).toBeVisible()

    const before = await readDeviceCommands(page, device.id)
    expect(before).toHaveLength(1)
    await page.getByRole('button', { name: 'Recover this submission' }).click()
    await expect(page.locator('.command-current').getByText('Completed', { exact: true })).toBeVisible({ timeout: 20_000 })
    const after = await readDeviceCommands(page, device.id)
    expect(after).toHaveLength(1)
    expect(after[0]?.id).toBe(before[0]?.id)
    expect(after[0]?.status).toBe('COMPLETED')
  })

  test('keeps ACK intermediate until the server records expiry', async ({ page }, testInfo) => {
    test.setTimeout(160_000)
    const device = await seedAlertDevice(page, uniqueKey(testInfo, 'ack-only'))
    await startCommandSimulator(device.id, 'ack-only')
    await page.goto(`/devices/${device.id}`)
    await createConfirmedPing(page)
    await expect(page.locator('.command-current').getByText('Acknowledged', { exact: true })).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('The device reported receipt. Its final result is still pending.')).toBeVisible()
    await expect(page.locator('.command-current').getByText('Completed', { exact: true })).toHaveCount(0)

    await expect.poll(async () => {
      const commands = await readDeviceCommands(page, device.id)
      return commands[0]?.status
  }, { timeout: 150_000, intervals: [1_000, 2_000, 2_000] }).toBe('TIMED_OUT')

    await expect(page.locator('.command-current').getByText('Timed out', { exact: true })).toBeVisible()
    await expect(page.getByText(/server recorded that no terminal device result arrived/i)).toBeVisible()
  })
})
