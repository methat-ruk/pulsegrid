import { screen } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { describe, expect, it } from 'vitest'

import PlannedState from '../../app/components/PlannedState.vue'

describe('PlannedState', () => {
  it('renders an honest foundation state without product metrics', async () => {
    await renderSuspended(PlannedState)

    expect(screen.getByRole('heading', { name: 'Product data is not connected yet.' })).toBeTruthy()
    expect(screen.getByText('The console foundation is ready for the first operator workflow.')).toBeTruthy()
    expect(screen.queryByText(/devices|telemetry|alerts|commands/i)).toBeNull()
  })
})
