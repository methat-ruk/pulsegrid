import { screen } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { describe, expect, it } from 'vitest'

import DeviceRegistryEntryPoint from '../../app/components/DeviceRegistryEntryPoint.vue'

describe('DeviceRegistryEntryPoint', () => {
  it('renders the first truthful product workflow entry point', async () => {
    await renderSuspended(DeviceRegistryEntryPoint)

    expect(screen.getByRole('heading', { name: 'Device registry' })).toBeTruthy()
    expect(screen.getByText('List, provision, and inspect devices in the development registry.')).toBeTruthy()
    expect(screen.getByRole('link', { name: /open devices/i }).getAttribute('href')).toBe('/devices')
  })
})
