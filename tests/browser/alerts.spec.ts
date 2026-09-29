import { createTemperatureRule, expect, publishSimulatorTelemetry, readAlerts, seedAlertDevice, test } from './fixtures'

function uniqueKey(testInfo: { workerIndex: number, retry: number }, suffix: string) {
  return `browser-alert-${Date.now()}-${testInfo.workerIndex}-${testInfo.retry}-${suffix}`
}

function mockOccurrence(id: string, deviceId = '22222222-2222-4222-8222-222222222222') {
  return {
    id,
    deviceId,
    ruleId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    messageId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
    observedAt: '2026-09-24T09:59:59Z',
    receivedAt: '2026-09-24T10:00:00Z',
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

  test('appends a next page and retries a failed page with the same cursor', async ({ page }) => {
    const cursors: Array<string | null> = []
    let nextPageAttempts = 0
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string, variables?: { after?: string | null } }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }

      const cursor = body.variables?.after ?? null
      cursors.push(cursor)
      if (cursor === null) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ data: { alerts: {
            edges: [{ cursor: 'opaque-page-one', node: mockOccurrence('11111111-1111-4111-8111-111111111111', '11111111-1111-4111-8111-111111111112') }],
            pageInfo: { endCursor: 'opaque-page-one', hasNextPage: true },
          } } }),
        })
        return
      }

      nextPageAttempts += 1
      if (nextPageAttempts === 1) {
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
        body: JSON.stringify({ data: { alerts: {
          edges: [{ cursor: 'opaque-page-two', node: mockOccurrence('22222222-2222-4222-8222-222222222223', '22222222-2222-4222-8222-222222222224') }],
          pageInfo: { endCursor: null, hasNextPage: false },
        } } }),
      })
    })

    await page.goto('/alerts')
    const rows = page.locator('.alert-table tbody tr')
    await expect(rows).toHaveCount(1)
    await page.getByRole('button', { name: 'Load more alerts' }).click()
    await expect(page.getByRole('alert')).toContainText('Previously loaded occurrences remain visible.')
    await expect(rows).toHaveCount(1)
    await expect(page.locator('.alert-table tbody')).toContainText('11111111-1111-4111-8111-111111111112')

    await page.getByRole('button', { name: 'Retry loading more' }).click()
    await expect(rows).toHaveCount(2)
    await expect(page.locator('.alert-table tbody')).toContainText('22222222-2222-4222-8222-222222222224')
    await expect(page.getByRole('button', { name: 'Load more alerts' })).toHaveCount(0)
    expect(cursors).toEqual([null, 'opaque-page-one', 'opaque-page-one'])
  })

  test('clears an in-flight device scope before showing the next scope', async ({ page }) => {
    const firstDeviceID = '11111111-1111-4111-8111-111111111112'
    const nextDeviceID = '22222222-2222-4222-8222-222222222224'
    let markFirstPageStarted!: () => void
    let releaseFirstPage!: () => void
    let markNextDeviceStarted!: () => void
    let releaseNextDevice!: () => void
    let markFirstPageSettled!: () => void
    const firstPageStarted = new Promise<void>((resolve) => { markFirstPageStarted = resolve })
    const firstPageResponse = new Promise<void>((resolve) => { releaseFirstPage = resolve })
    const nextDeviceStarted = new Promise<void>((resolve) => { markNextDeviceStarted = resolve })
    const nextDeviceResponse = new Promise<void>((resolve) => { releaseNextDevice = resolve })
    const firstPageSettled = new Promise<void>((resolve) => { markFirstPageSettled = resolve })
    const requestedScopes: string[] = []

    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as {
        query?: string
        variables?: { id?: string, deviceId?: string | null }
      }
      if (body.query?.includes('query Device')) {
        const id = body.variables?.id
        if (id === nextDeviceID) {
          markNextDeviceStarted()
          await nextDeviceResponse
        }
        const isNextDevice = id === nextDeviceID
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ data: { device: {
            id: id ?? '',
            deviceKey: isNextDevice ? 'next-device' : 'first-device',
            displayName: isNextDevice ? 'Next device' : 'First device',
            createdAt: '2026-09-24T10:00:00Z',
          } } }),
        })
        return
      }
      if (!body.query?.includes('query Alerts')) {
        await route.continue()
        return
      }

      const deviceId = body.variables?.deviceId ?? ''
      requestedScopes.push(deviceId)
      if (deviceId === firstDeviceID) {
        markFirstPageStarted()
        await firstPageResponse
        try {
          await route.fulfill({
            status: 200,
            contentType: 'application/graphql-response+json',
            body: JSON.stringify({ data: { alerts: {
              edges: [{ cursor: 'first-device-cursor', node: mockOccurrence('33333333-3333-4333-8333-333333333335', firstDeviceID) }],
              pageInfo: { endCursor: null, hasNextPage: false },
            } } }),
          })
        }
        catch {
          // The route change aborts this in-flight request.
        }
        finally {
          markFirstPageSettled()
        }
        return
      }

      await route.fulfill({
        status: 200,
        contentType: 'application/graphql-response+json',
        body: JSON.stringify({ data: { alerts: {
          edges: [{ cursor: 'next-device-cursor', node: mockOccurrence('44444444-4444-4444-8444-444444444445', nextDeviceID) }],
          pageInfo: { endCursor: null, hasNextPage: false },
        } } }),
      })
    })

    await page.goto(`/alerts?deviceId=${firstDeviceID}`)
    await firstPageStarted
    await page.evaluate((deviceID) => {
      const url = new URL(window.location.href)
      url.searchParams.set('deviceId', deviceID)
      window.history.pushState({}, '', url)
      window.dispatchEvent(new PopStateEvent('popstate'))
    }, nextDeviceID)
    await nextDeviceStarted
    await expect(page.getByText('Loading device alerts…')).toBeVisible()
    await expect(page.getByText(firstDeviceID, { exact: true })).toHaveCount(0)

    releaseFirstPage()
    await firstPageSettled
    await expect(page.getByText(firstDeviceID, { exact: true })).toHaveCount(0)
    releaseNextDevice()

    await expect(page.getByRole('heading', { name: 'Alerts for Next device' })).toBeVisible()
    await expect(page.locator('.alert-table tbody')).toContainText(nextDeviceID)
    await expect(page.locator('.alert-table tbody')).not.toContainText(firstDeviceID)
    expect(requestedScopes).toEqual([firstDeviceID, nextDeviceID])
  })

  test('keeps the stored snapshot visible when device-name lookup fails', async ({ page }) => {
    const alert = mockOccurrence('55555555-5555-4555-8555-555555555556')
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string }
      if (body.query?.includes('query Alert')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ data: { alert } }),
        })
        return
      }
      if (body.query?.includes('query Device')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ errors: [{ message: 'unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }] }),
        })
        return
      }
      await route.continue()
    })

    await page.goto(`/alerts/${alert.id}`)
    await expect(page.getByRole('heading', { name: 'Triggering context' })).toBeVisible()
    await expect(page.getByText('Device name is unavailable; the stored device identifier remains available.')).toBeVisible()
    await expect(page.locator('.alert-detail-card')).toContainText(alert.deviceId)
    await expect(page.locator('.alert-detail-card')).toContainText(alert.ruleId)
    await expect(page.locator('.alert-detail-card')).toContainText(alert.messageId)
    await expect(page.getByText('Temperature: 31 °C is greater than 25 °C')).toBeVisible()
    await expect(page.locator('.alert-detail-card')).toContainText('Observed')
    await expect(page.locator('.alert-detail-card')).toContainText('Received')
    await expect(page.locator('.alert-detail-card')).toContainText('Recorded')
    await expect(page.getByRole('link', { name: alert.deviceId })).toHaveAttribute('href', `/devices/${alert.deviceId}`)
  })

  test('focuses alert detail and reflows its snapshot at 390 px', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The repository currently runs one Chromium browser project')
    await page.setViewportSize({ width: 390, height: 844 })
    const alert = mockOccurrence('66666666-6666-4666-8666-666666666667')
    await page.route('**/api/graphql', async (route) => {
      const body = route.request().postDataJSON() as { query?: string, variables?: { id?: string } }
      if (body.query?.includes('query Alert')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ data: { alert } }),
        })
        return
      }
      if (body.query?.includes('query Device')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/graphql-response+json',
          body: JSON.stringify({ data: { device: {
            id: body.variables?.id ?? alert.deviceId,
            deviceKey: 'responsive-device',
            displayName: 'Responsive device',
            createdAt: '2026-09-24T10:00:00Z',
          } } }),
        })
        return
      }
      await route.continue()
    })

    await page.goto(`/alerts/${alert.id}`)
    const heading = page.getByRole('heading', { name: 'Triggering context' })
    await expect(heading).toBeVisible()
    await expect(heading).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name: 'Responsive device' })).toBeFocused()

    const layout = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0),
    }))
    const columns = await page.locator('.alert-detail-card').evaluate(element => getComputedStyle(element).gridTemplateColumns)
    expect(layout.clientWidth).toBe(390)
    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth)
    expect(columns.trim().split(/\s+/u)).toHaveLength(1)
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
