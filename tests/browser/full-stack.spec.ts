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
