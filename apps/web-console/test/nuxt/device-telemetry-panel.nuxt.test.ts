import { fireEvent, screen, waitFor, cleanup } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DeviceTelemetryPanel from '../../app/components/DeviceTelemetryPanel.vue'

const DEVICE_ID = '11111111-1111-4111-8111-111111111111'

const mocks = vi.hoisted(() => ({
  getDeviceTelemetryOverview: vi.fn(),
  getDeviceTelemetryPage: vi.fn(),
}))

vi.mock('../../app/features/devices/device-graphql', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../app/features/devices/device-graphql')>()
  return {
    ...actual,
    getDeviceTelemetryOverview: mocks.getDeviceTelemetryOverview,
    getDeviceTelemetryPage: mocks.getDeviceTelemetryPage,
  }
})

function overview(currentState: Record<string, unknown> | null, points: Array<Record<string, unknown>>, hasNextPage = false) {
  return {
    deviceCurrentState: currentState,
    deviceTelemetry: {
      edges: points.map((node, index) => ({ cursor: `cursor-${index + 1}`, node })),
      pageInfo: { endCursor: hasNextPage ? 'cursor-next' : null, hasNextPage },
    },
  }
}

function current(lastSeenAt = new Date().toISOString()) {
  return {
    messageId: 'message-current',
    observedAt: '2026-09-22T04:00:00Z',
    receivedAt: '2026-09-22T04:00:01Z',
    temperatureCelsius: 23.5,
    lastSeenAt,
  }
}

function point(messageId: string, temperatureCelsius: number, observedAt: string) {
  return {
    messageId,
    observedAt,
    receivedAt: new Date(Date.parse(observedAt) + 1000).toISOString(),
    temperatureCelsius,
  }
}

describe('device telemetry panel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.getDeviceTelemetryPage.mockResolvedValue({
      deviceTelemetry: {
        edges: [],
        pageInfo: { endCursor: null, hasNextPage: false },
      },
    })
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('shows loading then an honest empty state', async () => {
    let resolveOverview!: (value: ReturnType<typeof overview>) => void
    mocks.getDeviceTelemetryOverview.mockReturnValue(new Promise((resolve) => {
      resolveOverview = resolve
    }))

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    expect(screen.getByRole('status').textContent).toContain('Loading telemetry…')

    resolveOverview(overview(null, []))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'No telemetry yet.' })).toBeTruthy())
    expect(screen.getByRole('button', { name: 'Refresh telemetry' })).toBeTruthy()
  })

  it('derives recent and stale signal labels from lastSeenAt', async () => {
    const recent = new Date().toISOString()
    mocks.getDeviceTelemetryOverview.mockResolvedValue(overview(
      current(recent),
      [point('message-1', 23.5, '2026-09-22T04:00:00Z')],
    ))

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    await waitFor(() => expect(screen.getByText('Recent signal')).toBeTruthy())
    expect(screen.getByRole('heading', { name: '23.5°C' })).toBeTruthy()
    expect(screen.queryByRole('img')).toBeNull()

    mocks.getDeviceTelemetryOverview.mockResolvedValue(overview(
      current(new Date(Date.now() - 6 * 60 * 1000).toISOString()),
      [point('message-2', 18, '2026-09-22T04:01:00Z')],
    ))
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh telemetry' }))
    await waitFor(() => expect(screen.getByText('Stale signal')).toBeTruthy())
  })

  it('preserves the previous data when refresh fails', async () => {
    mocks.getDeviceTelemetryOverview
      .mockResolvedValueOnce(overview(current(), [point('message-1', 23.5, '2026-09-22T04:00:00Z')]))
      .mockRejectedValueOnce(new Error('network down'))

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    await waitFor(() => expect(screen.getByRole('heading', { name: '23.5°C' })).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Refresh telemetry' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('previous telemetry remains visible'))
    expect(screen.getByRole('heading', { name: '23.5°C' })).toBeTruthy()
  })

  it('surfaces a refresh failure even when the previous state was empty', async () => {
    mocks.getDeviceTelemetryOverview
      .mockResolvedValueOnce(overview(null, []))
      .mockRejectedValueOnce(new Error('network down'))

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'No telemetry yet.' })).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Refresh telemetry' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('previous telemetry remains visible'))
    expect(screen.getByRole('heading', { name: 'No telemetry yet.' })).toBeTruthy()
  })

  it('appends a history continuation using the server cursor', async () => {
    mocks.getDeviceTelemetryOverview.mockResolvedValue(overview(
      current(),
      [point('message-1', 23.5, '2026-09-22T04:00:00Z')],
      true,
    ))
    mocks.getDeviceTelemetryPage.mockResolvedValue({
      deviceTelemetry: {
        edges: [{
          cursor: 'cursor-2',
          node: point('message-2', 24.5, '2026-09-22T03:59:00Z'),
        }],
        pageInfo: { endCursor: null, hasNextPage: false },
      },
    })

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Load more history' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Load more history' }))

    await waitFor(() => expect(screen.getByText('2 loaded')).toBeTruthy())
    expect(mocks.getDeviceTelemetryPage).toHaveBeenCalledWith(DEVICE_ID, 50, 'cursor-next', expect.any(AbortSignal))
    expect(screen.getByText('24.5°C')).toBeTruthy()
  })

  it('clears an aborted load-more state when refresh replaces the page', async () => {
    mocks.getDeviceTelemetryOverview
      .mockResolvedValueOnce(overview(current(), [point('message-1', 23.5, '2026-09-22T04:00:00Z')], true))
      .mockResolvedValueOnce(overview(current(), [point('message-2', 24.5, '2026-09-22T04:01:00Z')]))
    mocks.getDeviceTelemetryPage.mockReturnValue(new Promise(() => {}))

    await renderSuspended(DeviceTelemetryPanel, { props: { deviceId: DEVICE_ID } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Load more history' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Load more history' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh telemetry' }))

    await waitFor(() => expect(screen.getByText('24.5°C')).toBeTruthy())
    expect(screen.queryByRole('button', { name: 'Load more history' })).toBeNull()
    expect((screen.getByRole('button', { name: 'Refresh telemetry' }) as HTMLButtonElement).disabled).toBe(false)
  })
})
