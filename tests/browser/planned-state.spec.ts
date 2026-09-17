import { expect, test } from './fixtures'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 834, height: 1112 },
  { name: 'mobile', width: 390, height: 844 },
  { name: 'narrow-mobile', width: 320, height: 568 },
] as const

for (const viewport of viewports) {
  test.describe(`${viewport.name} console shell`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } })

    test('renders without browser errors or horizontal overflow', async ({ page }) => {
      const consoleErrors: string[] = []
      const pageErrors: string[] = []

      page.on('console', (message) => {
        if (message.type() === 'error') consoleErrors.push(message.text())
      })
      page.on('pageerror', (error) => pageErrors.push(error.message))

      await page.goto('/')
      await expect(page).toHaveTitle('PulseGrid Console')
      await expect(page.getByRole('heading', { name: 'PulseGrid Console' })).toBeVisible()
      await expect(page.getByRole('heading', { name: 'Device registry' })).toBeVisible()
      await expect(page.getByRole('link', { name: 'Open devices' })).toBeVisible()
      await expect(page.getByRole('status')).toHaveText('Local API is ready.')

      const dimensions = await page.evaluate(() => ({
        clientWidth: document.documentElement.clientWidth,
        scrollWidth: Math.max(
          document.documentElement.scrollWidth,
          document.body?.scrollWidth ?? 0,
        ),
      }))
      expect(dimensions.scrollWidth, `horizontal overflow at ${viewport.name}`).toBeLessThanOrEqual(dimensions.clientWidth)
      expect(consoleErrors, `console errors at ${viewport.name}`).toEqual([])
      expect(pageErrors, `page errors at ${viewport.name}`).toEqual([])
    })

    test('supports the primary keyboard path', async ({ page }) => {
      await page.goto('/')

      const skipLink = page.getByRole('link', { name: 'Skip to main content' })
      await skipLink.focus()
      await expect(skipLink).toBeFocused()
      await skipLink.press('Enter')
      await expect(page).toHaveURL(/#main-content$/)

      if (viewport.width >= 768) {
        const toggle = page.getByRole('button', { name: 'Collapse sidebar' })
        await toggle.focus()
        await toggle.press('Enter')
        const expandToggle = page.getByRole('button', { name: 'Expand sidebar' })
        await expect(expandToggle).toHaveAttribute('aria-expanded', 'false')
        await expandToggle.press('Enter')
        await expect(page.getByRole('button', { name: 'Collapse sidebar' })).toHaveAttribute('aria-expanded', 'true')
      }
      else {
        const menuButton = page.getByRole('button', { name: 'Menu' })
        await menuButton.focus()
        await menuButton.press('Enter')
        const menu = page.getByRole('dialog', { name: 'Mobile navigation' })
        await expect(menu).toBeVisible()
        await expect(menu.getByRole('link', { name: 'Devices', exact: true })).toBeVisible()
        await expect(menu.getByRole('button', { name: 'Close navigation' })).toBeFocused()
        await page.keyboard.press('Tab')
        await expect(menu.getByRole('link', { name: 'Overview' })).toBeFocused()
        await page.keyboard.press('Escape')
        await expect(menu).toBeHidden()
        await expect(menuButton).toBeFocused()
      }
    })
  })
}
