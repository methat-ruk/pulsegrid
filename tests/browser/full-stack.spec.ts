import { expect, test } from './fixtures'

test.describe('full-stack API readiness', () => {
  test('shows a bounded checking state while the readiness request is pending', async ({ page }) => {
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })

    await page.route('**/api/operational/ready', async (route) => {
      await pending
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ready' }),
      })
    })

    const navigation = page.goto('/')
    await expect(page.getByRole('status')).toHaveText('Checking the local API…')
    release()
    await navigation
    await expect(page.getByRole('status')).toHaveText('Local API is ready.')
  })

  test('maps a hung readiness request to unavailable with a retry action', async ({ page }) => {
    await page.route('**/api/operational/ready', async (route) => {
      await new Promise(resolve => setTimeout(resolve, 4_000))
      try {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'ready' }),
        })
      }
      catch {
        // The client timeout is expected to abort this request first.
      }
    })

    await page.goto('/')
    await expect(page.getByRole('status')).toHaveText('Checking the local API…')
    await expect(page.getByRole('status')).toHaveText('Local API is unavailable.', { timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Retry connection' })).toBeVisible()
  })

  test('does not let an unmounted readiness request update the next page', async ({ page }) => {
    let releaseFirst!: () => void
    const firstPending = new Promise<void>((resolve) => {
      releaseFirst = resolve
    })
    let markFirstRequestStarted!: () => void
    const firstRequestStarted = new Promise<void>((resolve) => {
      markFirstRequestStarted = resolve
    })
    let requestCount = 0

    await page.route('**/api/operational/ready', async (route) => {
      requestCount += 1
      if (requestCount === 1) {
        markFirstRequestStarted()
        await firstPending
        try {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ status: 'ready' }),
          })
        }
        catch {
          // The first page is expected to abort this request during navigation.
        }
        return
      }

      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'unavailable' }),
      })
    })

    await page.goto('/')
    await expect(page.getByRole('status')).toHaveText('Checking the local API…')
    await firstRequestStarted
    await page.goto('about:blank')
    await page.goto('/')
    await expect.poll(() => requestCount).toBeGreaterThan(1)
    await expect(page.getByRole('status')).toHaveText('Local API is unavailable.')

    releaseFirst()
    await expect(page.getByRole('status')).toHaveText('Local API is unavailable.')
  })

  test('recovers after the Go process is stopped and restarted', async ({ page, apiProcess }) => {
    await page.goto('/')
    await expect(page.getByRole('status')).toHaveText('Local API is ready.')

    const readiness = await page.request.get('/api/operational/ready')
    expect(readiness.status()).toBe(200)
    expect(readiness.headers()['cache-control']).toBe('no-store')
    await expect(readiness.json()).resolves.toEqual({ status: 'ready' })

    await apiProcess.stop()
    await page.reload()
    await expect(page.getByRole('status')).toHaveText('Local API is unavailable.')
    const unavailable = await page.request.get('/api/operational/ready')
    expect(unavailable.status()).toBe(503)
    await expect(unavailable.json()).resolves.toEqual({ status: 'unavailable' })
    await page.getByRole('button', { name: 'Retry connection' }).click()
    await expect(page.getByRole('status')).toHaveText('Local API is unavailable.')

    await apiProcess.start()
    await page.getByRole('button', { name: 'Retry connection' }).click()
    await expect(page.getByRole('status')).toHaveText('Local API is ready.')
  })
})
