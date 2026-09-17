import { fireEvent, screen, waitFor, cleanup } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DevicesPage from '../../app/pages/devices/index.vue'
import NewDevicePage from '../../app/pages/devices/new.vue'

const mocks = vi.hoisted(() => ({
  listDevices: vi.fn(),
  getDevice: vi.fn(),
  createDevice: vi.fn(),
}))

vi.mock('../../app/features/devices/device-graphql', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../app/features/devices/device-graphql')>()
  return {
    ...actual,
    listDevices: mocks.listDevices,
    getDevice: mocks.getDevice,
    createDevice: mocks.createDevice,
  }
})

const emptyPage = {
  devices: {
    edges: [],
    pageInfo: { endCursor: null, hasNextPage: false },
  },
}

describe('device pages', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.listDevices.mockResolvedValue(emptyPage)
  })

  afterEach(() => {
    cleanup()
  })

  it('shows loading and empty list states from the list client', async () => {
    let resolveList!: (value: typeof emptyPage) => void
    mocks.listDevices.mockReturnValue(new Promise((resolve) => {
      resolveList = resolve
    }))

    await renderSuspended(DevicesPage)
    expect(screen.getByRole('status').textContent).toContain('Loading devices…')

    resolveList(emptyPage)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'No devices yet.' })).toBeTruthy())
  })

  it('shows a retryable error state when listing fails', async () => {
    mocks.listDevices.mockRejectedValue(new Error('network down'))

    await renderSuspended(DevicesPage)

    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('The device list could not be loaded. Try again.'))
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })

  it('blocks invalid Unicode input before creating a device', async () => {
    await renderSuspended(NewDevicePage)

    await fireEvent.update(screen.getByLabelText('Device key'), '😀'.repeat(129))
    await fireEvent.update(screen.getByLabelText('Display name'), 'Temperature')
    await fireEvent.click(screen.getByRole('button', { name: 'Create device' }))

    expect(screen.getByText('Device key must be 128 characters or fewer.')).toBeTruthy()
    expect(mocks.createDevice).not.toHaveBeenCalled()
  })
})
