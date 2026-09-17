import type { Page } from '@playwright/test'

import { expect, test } from './fixtures'

const createDeviceMutation = `mutation CreateDevice($input: CreateDeviceInput!) {
  createDevice(input: $input) {
    id
    deviceKey
    displayName
    createdAt
  }
}`

async function seedDevice(page: Page, deviceKey: string, displayName: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: createDeviceMutation,
      variables: { input: { deviceKey, displayName } },
    },
  })
  expect(response.status()).toBe(200)
  const payload = await response.json()
  expect(payload.errors).toBeUndefined()
  return payload.data.createDevice as { id: string; deviceKey: string }
}

function uniqueKey(testInfo: { workerIndex: number; retry: number }, suffix: string) {
  return `browser-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}-${suffix}`
}

test.describe('device registry journey', () => {
  test('lists a real page and continues with the opaque cursor', async ({ page }, testInfo) => {
    await page.goto('/devices')
    await expect(page.getByRole('heading', { name: 'No devices yet.' })).toBeVisible()

    const keys: string[] = []
    for (const index of Array.from({ length: 21 }, (_, value) => value)) {
      const key = uniqueKey(testInfo, `page-${index}`)
      await seedDevice(page, key, `Browser pagination ${index}`)
      keys.push(key)
    }
    const oldestKey = keys[0]

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Devices' })).toBeVisible()
    await expect(page.locator('.device-table tbody').getByText(oldestKey)).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Load more' })).toBeVisible()

    await page.getByRole('button', { name: 'Load more' }).click()
    await expect(page.locator('.device-table tbody').getByText(oldestKey)).toBeVisible()
  })

  test('creates, inspects, and reports a duplicate without losing form input', async ({ page }, testInfo) => {
    const deviceKey = uniqueKey(testInfo, 'create')
    const displayName = 'Browser-created device'

    await page.goto('/devices/new')
    await page.getByLabel('Device key').fill(deviceKey)
    await page.getByLabel('Display name').fill(displayName)
    await page.getByRole('button', { name: 'Create device' }).click()

    await expect(page).toHaveURL(/\/devices\/[0-9a-f-]{36}$/)
    await expect(page.getByRole('heading', { name: displayName })).toBeVisible()
    await expect(page.locator('.detail-card').getByText(deviceKey)).toBeVisible()

    await page.goto('/devices/new')
    await page.getByLabel('Device key').fill(deviceKey)
    await page.getByLabel('Display name').fill(displayName)
    await page.getByRole('button', { name: 'Create device' }).click()

    await expect(page.getByRole('alert')).toHaveText('A device with this key already exists.')
    await expect(page.getByLabel('Device key')).toHaveValue(deviceKey)
    await expect(page.getByLabel('Display name')).toHaveValue(displayName)
  })

  test('keeps malformed identifiers in a safe error state', async ({ page }) => {
    await page.goto('/devices/not-a-canonical-id')
    await expect(page.getByRole('heading', { name: 'Device could not be loaded.' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible()
  })

  test('renders a null GraphQL device as not found', async ({ page }) => {
    await page.goto('/devices/00000000-0000-4000-8000-000000000000')
    await expect(page.getByRole('heading', { name: 'Device not found.' })).toBeVisible()
  })
})
