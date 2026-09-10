import { fireEvent, screen, within } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { h } from 'vue'
import { describe, expect, it } from 'vitest'

import DefaultLayout from '../../app/layouts/default.vue'

describe('default layout', () => {
  it('collapses and expands the desktop sidebar with an accessible toggle', async () => {
    const { container } = await renderSuspended(DefaultLayout, {
      slots: {
        default: () => h('div', 'content'),
      },
    })

    const sidebar = container.querySelector('aside')
    const collapseButton = screen.getByRole('button', { name: 'Collapse sidebar' })

    expect(collapseButton.getAttribute('aria-expanded')).toBe('true')
    expect(sidebar?.classList.contains('app-sidebar--collapsed')).toBe(false)

    await fireEvent.click(collapseButton)

    const expandButton = screen.getByRole('button', { name: 'Expand sidebar' })
    const overviewLink = screen.getByRole('link', { name: 'Overview' })

    expect(expandButton.getAttribute('aria-expanded')).toBe('false')
    expect(sidebar?.classList.contains('app-sidebar--collapsed')).toBe(true)
    expect(overviewLink.getAttribute('title')).toBe('Overview')
    expect(within(sidebar as HTMLElement).queryByRole('link', { name: 'PulseGrid home' })).toBeNull()

    await fireEvent.click(expandButton)

    expect(within(sidebar as HTMLElement).getByRole('link', { name: 'PulseGrid home' })).toBeTruthy()
  })
})
