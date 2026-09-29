import { createTemperatureRule, expect, publishSimulatorTelemetry, readAlerts, seedAlertDevice, test } from './fixtures'

function uniqueKey(testInfo: { workerIndex: number, retry: number }, suffix: string) {
  return `browser-alert-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}-${suffix}`
}

function mockOccurrence(id: string, deviceId = '22222222-2222-4222-8222-222222222222') {
  return {
    id,
    deviceId,
    temperatureCelsius: 31,
    metric: 'TEMPERATURE_CELSIUS',
    comparator: 'GT',
    thresholdCelsius: 25,
    createdAt: '2026-09-24T10:00:01Z',
  }
}

test.describe('alert console journey', () => {
  test('discovers a simulator-triggered occurrence and follows its stored context', async ({ page }, testInfo) => {
    const device = await seedAlertDevice(page, uniqueKey(testInfo, 'alert-journey'))
    const rule = await createTemperatureRule(page, device.id)
    expect(rule.enabled).toBe(true)
    const published = await publishSimulatorTelemetry(device.id, 31)
    await expect.poll(async () => (await readAlerts(page, device.id)).edges.length, { timeout: 10_000 }).toBe(1)
    const storedAlert = (await readAlerts(page, device.id)).edges[0].node
    expect(storedAlert).toMatchObject({
      deviceId: device.id,
      ruleId: rule.id,
      messageId: published.messageID,
      temperatureCelsius: 31,
      comparator: 'GT',
      thresholdCelsius: 25,
    })

    const graphqlOperations: string[] = []
    page.on('request', (request) => {
      if (request.url().endsWith('/api/graphql')) {
        const body = request.postDataJSON() as { query?: string }
        if (body.query) graphqlOperations.push(body.query)
      }
    })

    await page.goto('/alerts')
    await expect(page.getByRole('heading', { name: 'Alert occurrences' })).toBeVisible()
    await expect(page.locator('.alert-table tbody')).toContainText('Temperature: 31 °C is greater than 25 °C')
    await expect(page.locator('.alert-table tbody').getByText(device.id)).toBeVisible()
    await page.locator('.alert-table tbody').getByRole('link', { name: 'View details' }).click()
    await expect(page).toHaveURL(new RegExp(`/alerts/${storedAlert.id}$`))
    await expect(page.getByRole('heading', { name: 'Triggering context' })).toBeVisible()
    await expect(page.getByText('Temperature: 31 °C is greater than 25 °C')).toBeVisible()
    await expect(page.getByText('Observed', { exact: true })).toBeVisible()
    await expect(page.getByText('Received', { exact: true })).toBeVisible()
    await expect(page.getByText('Recorded', { exact: true })).toBeVisible()
    await expect(page.getByText(device.displayName)).toBeVisible()
    expect(graphqlOperations.some(query => query.includes('deviceTelemetry') || query.includes('thresholdRules'))).toBe(false)

    await page.getByRole('link', { name: device.displayName }).click()
    await expect(page).toHaveURL(new RegExp(`/devices/${device.id}$`))
    await expect(page.getByRole('heading', { name: device.displayName })).toBeVisible()
    await page.getByRole('link', { name: `View alerts for ${device.deviceKey}` }).click()
    await expect(page).toHaveURL(url => {
      const current = new URL(url)
      return current.pathname === '/alerts' && current.searchParams.get('deviceId') === device.id
    })
    await expect(page.getByRole('heading', { name: `Alerts for ${device.displayName}` })).toBeVisible()
    await expect(page.locator('.alert-table tbody')).toContainText(device.id)
    await page.goto(`/alerts/${storedAlert.id}`)
    await expect(page.getByRole('heading', { name: 'Triggering context' })).toBeVisible()
  })

  test('renders one not-found state for an unknown alert', async ({ page }) => {
    await page.goto('/alerts/00000000-0000-4000-8000-000000000000')
    await expect(page.getByRole('heading', { name: 'Alert not found.' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Back to alerts' }).first()).toBeVisible()
  })

  test('renders a narrow occurrence card without viewport overflow', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 320, height: 844 })
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({
          data: {
            alerts: {
              edges: [{ cursor: 'opaque-cursor', node: mockOccurrence('11111111-1111-4111-8111-111111111111') }],
              pageInfo: { endCursor: 'opaque-cursor', hasNextPage: false },
            },
          },
        }),
      })
    })

    await page.goto('/alerts')
    await expect(page.getByRole('list', { name: 'Recent alert occurrences, newest recorded first' })).toBeVisible()
    await expect(page.locator('.alert-card')).toContainText('Temperature: 31 °C is greater than 25 °C')
    await expect(page.locator('.alert-table-wrap')).toBeHidden()
    const dimensions = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth)
  })

  test('does not confirm an unknown device filter and shows an empty state', async ({ page }) => {
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({ data: { alerts: { edges: [], pageInfo: { endCursor: null, hasNextPage: false } } } }),
      })
    })
    await page.goto('/alerts?deviceId=00000000-0000-4000-8000-000000000000')
    await expect(page.getByRole('heading', { name: 'Device alerts are unavailable.' })).toBeVisible()

    await page.goto('/alerts')
    await expect(page.getByRole('heading', { name: 'No alert occurrences yet.' })).toBeVisible()
  })

  test('recovers from initial service failure and preserves an empty list after refresh failure', async ({ page }) => {
    let alertCalls = 0
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }
      alertCalls += 1
      if (alertCalls === 1 || alertCalls === 3) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ errors: [{ message: 'unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }] }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({ data: { alerts: { edges: [], pageInfo: { endCursor: null, hasNextPage: false } } } }),
      })
    })

    await page.goto('/alerts')
    await expect(page.getByRole('heading', { name: 'Alerts could not be loaded.' })).toBeVisible()
    await page.getByRole('button', { name: 'Try again' }).click()
    await expect(page.getByRole('heading', { name: 'No alert occurrences yet.' })).toBeVisible()
    await page.getByRole('button', { name: 'Refresh alerts' }).click()
    await expect(page.getByRole('alert')).toContainText('The last loaded list is still empty.')
    await expect(page.getByRole('heading', { name: 'No alert occurrences yet.' })).toBeVisible()
  })

  test('preserves occurrences on failed refresh and replaces them after retry', async ({ page }) => {
    let alertCalls = 0
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }
      alertCalls += 1
      if (alertCalls === 2) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ errors: [{ message: 'unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }] }),
        })
        return
      }
      const occurrence = mockOccurrence(
        alertCalls === 1 ? '11111111-1111-4111-8111-111111111111' : '33333333-3333-4333-8333-333333333333',
        alertCalls === 1 ? '11111111-1111-4111-8111-111111111112' : '33333333-3333-4333-8333-333333333334',
      )
      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({ data: { alerts: { edges: [{ cursor: `cursor-${alertCalls}`, node: occurrence }], pageInfo: { endCursor: null, hasNextPage: false } } } }),
      })
    })

    await page.goto('/alerts')
    await expect(page.locator('.alert-table tbody tr')).toHaveCount(1)
    await page.getByRole('button', { name: 'Refresh alerts' }).click()
    await expect(page.getByRole('alert')).toContainText('Previously loaded occurrences remain visible.')
    await expect(page.locator('.alert-table tbody')).toContainText('11111111-1111-4111-8111-111111111112')
    await page.getByRole('button', { name: 'Retry refresh' }).click()
    await expect(page.locator('.alert-table tbody')).toContainText('33333333-3333-4333-8333-333333333334')
    await expect(page.locator('.alert-table tbody')).not.toContainText('11111111-1111-4111-8111-111111111112')
  })

  test('cancels a pending next page when Refresh starts and keeps the new head page', async ({ page }) => {
    let firstPageRequests = 0
    let markLoadMoreStarted!: () => void
    let markRefreshStarted!: () => void
    let releaseRefresh!: () => void
    const loadMoreStarted = new Promise<void>((resolve) => { markLoadMoreStarted = resolve })
    const refreshStarted = new Promise<void>((resolve) => { markRefreshStarted = resolve })
    const refreshResponse = new Promise<void>((resolve) => { releaseRefresh = resolve })

    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string, variables?: { after?: string | null } }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }
      if (body.variables?.after) {
        markLoadMoreStarted()
        await new Promise(resolve => setTimeout(resolve, 250))
        try {
          await route.fulfill({
            status: 200,
            contentType: 'application/graphql-response+json',
            body: JSON.stringify({ data: { alerts: { edges: [{ cursor: 'stale', node: mockOccurrence('44444444-4444-4444-8444-444444444444', '44444444-4444-4444-8444-444444444445') }], pageInfo: { endCursor: 'stale', hasNextPage: false } } } }),
          })
        }
        catch {
          // Refresh aborts this continuation request.
        }
        return
      }

      firstPageRequests += 1
      if (firstPageRequests === 2) {
        markRefreshStarted()
        await refreshResponse
      }
      const occurrence = mockOccurrence(
        firstPageRequests === 1 ? '11111111-1111-4111-8111-111111111111' : '33333333-3333-4333-8333-333333333333',
        firstPageRequests === 1 ? '11111111-1111-4111-8111-111111111112' : '33333333-3333-4333-8333-333333333334',
      )
      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({ data: { alerts: {
          edges: [{ cursor: 'opaque-next-page', node: occurrence }],
          pageInfo: { endCursor: 'opaque-next-page', hasNextPage: true },
        } } }),
      })
    })

    await page.goto('/alerts')
    await expect(page.locator('.alert-table tbody')).toContainText('11111111-1111-4111-8111-111111111112')
    await page.getByRole('button', { name: 'Load more alerts' }).click()
    await loadMoreStarted
    await page.getByRole('button', { name: 'Refresh alerts' }).click()
    await refreshStarted
    await expect(page.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Load more alerts' })).toBeDisabled()
    releaseRefresh()
    await expect(page.locator('.alert-table tbody')).toContainText('33333333-3333-4333-8333-333333333334')
    await page.waitForTimeout(300)
    await expect(page.locator('.alert-table tbody')).not.toContainText('44444444-4444-4444-8444-444444444445')
  })
})
