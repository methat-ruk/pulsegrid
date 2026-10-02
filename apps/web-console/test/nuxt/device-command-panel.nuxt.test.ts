import { fireEvent, screen, waitFor, cleanup } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DeviceCommandPanel from '../../app/components/DeviceCommandPanel.vue'
import type { CommandSnapshot } from '../../app/features/commands/command-graphql'

const DEVICE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'
const OTHER_COMMAND_ID = '44444444-4444-4444-8444-444444444444'
const IDEMPOTENCY_KEY = '33333333-3333-4333-8333-333333333333'

const mocks = vi.hoisted(() => ({
  createCommand: vi.fn(),
  getCommand: vi.fn(),
  listDeviceCommands: vi.fn(),
}))

vi.mock('../../app/features/commands/command-graphql', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../app/features/commands/command-graphql')>()
  return {
    ...actual,
    createCommand: mocks.createCommand,
    getCommand: mocks.getCommand,
    listDeviceCommands: mocks.listDeviceCommands,
  }
})

function command(id = COMMAND_ID, status: CommandSnapshot['status'] = 'PENDING'): CommandSnapshot {
  const terminal = status === 'COMPLETED' || status === 'FAILED' || status === 'TIMED_OUT'
  const timestamp = status === 'PENDING' ? '2026-10-02T10:00:00.000Z' : '2026-10-02T10:00:02.000Z'
  return {
    id,
    deviceId: DEVICE_ID,
    type: 'PING',
    status,
    createdAt: '2026-10-02T10:00:00.000Z',
    updatedAt: timestamp,
    expiresAt: '2026-10-02T10:02:00.000Z',
    dispatchedAt: status === 'PENDING' ? null : '2026-10-02T10:00:01.000Z',
    acknowledgedAt: status === 'ACKNOWLEDGED' || status === 'COMPLETED' ? '2026-10-02T10:00:01.500Z' : null,
    terminalAt: terminal ? timestamp : null,
    failureCode: status === 'FAILED' ? 'DEVICE_REPORTED_FAILURE' : null,
  }
}

function emptyHistory() {
  return { deviceCommands: { edges: [], pageInfo: { endCursor: null, hasNextPage: false } } }
}

describe('device command panel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    const data = new Map<string, string>()
    const storage: Storage = {
      getItem: key => data.get(key) ?? null,
      setItem: (key, value) => { data.set(key, String(value)) },
      removeItem: (key) => { data.delete(key) },
      clear: () => { data.clear() },
      key: index => [...data.keys()][index] ?? null,
      get length() { return data.size },
    }
    vi.spyOn(window, 'sessionStorage', 'get').mockReturnValue(storage)
    mocks.listDeviceCommands.mockResolvedValue(emptyHistory())
    mocks.getCommand.mockImplementation(async (id: string) => command(id))
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('requires explicit confirmation and shows a terminal server result', async () => {
    mocks.createCommand.mockResolvedValue(command(COMMAND_ID, 'COMPLETED'))
    mocks.getCommand.mockResolvedValue(command(COMMAND_ID, 'COMPLETED'))
    vi.spyOn(window.crypto, 'randomUUID').mockReturnValue(IDEMPOTENCY_KEY)

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    expect(screen.getByRole('heading', { name: 'Confirm diagnostic PING' })).toBeTruthy()
    expect(screen.getByText(/acceptance means the intent was stored/i)).toBeTruthy()
    expect(mocks.createCommand).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(mocks.createCommand).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm and send PING' }))
    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Completed'))
    expect(mocks.createCommand).toHaveBeenCalledTimes(1)
    expect(mocks.createCommand).toHaveBeenCalledWith(DEVICE_ID, IDEMPOTENCY_KEY, expect.any(AbortSignal))
    expect(screen.getByText('The device reported that the diagnostic PING completed.')).toBeTruthy()
    expect(screen.getByText(/Terminal result/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy()
    expect(readStoredRecord()).toBeNull()
  })

  it('recovers an uncertain submission with the original idempotency key', async () => {
    mocks.createCommand
      .mockRejectedValueOnce(new Error('response lost'))
      .mockResolvedValueOnce(command())
    vi.spyOn(window.crypto, 'randomUUID').mockReturnValue(IDEMPOTENCY_KEY)

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm and send PING' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Recover this submission' })).toBeTruthy())
    expect(screen.queryByRole('button', { name: 'Send PING' })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'Recover this submission' }))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'PING' })).toBeTruthy())
    expect(mocks.createCommand).toHaveBeenCalledTimes(2)
    expect(mocks.createCommand.mock.calls[0]?.slice(0, 2)).toEqual(mocks.createCommand.mock.calls[1]?.slice(0, 2))
    expect(readStoredRecord()).toMatchObject({ idempotencyKey: IDEMPOTENCY_KEY, commandId: COMMAND_ID })
  })

  it('does not let a stale history refresh overwrite a newer confirmed command state', async () => {
    mocks.listDeviceCommands
      .mockResolvedValueOnce(emptyHistory())
      .mockResolvedValueOnce({
        deviceCommands: {
          edges: [{ cursor: 'stale', node: command(COMMAND_ID, 'PENDING') }],
          pageInfo: { endCursor: 'stale', hasNextPage: true },
        },
      })
    mocks.createCommand.mockResolvedValue(command(COMMAND_ID, 'FAILED'))
    mocks.getCommand.mockResolvedValue(command(COMMAND_ID, 'FAILED'))
    vi.spyOn(window.crypto, 'randomUUID').mockReturnValue(IDEMPOTENCY_KEY)

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm and send PING' }))

    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Failed'))
    await waitFor(() => expect(document.querySelector('.command-history-row .command-status')?.textContent).toContain('Failed'))
    expect(document.querySelector('.command-history-row .command-status')?.textContent).not.toContain('Pending')
    expect(screen.getByText(/older commands exist but are not shown/i)).toBeTruthy()
  })

  it('labels dispatch and acknowledgement as intermediate states', async () => {
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [
          { cursor: 'dispatched', node: command() },
          { cursor: 'acknowledged', node: command(OTHER_COMMAND_ID, 'ACKNOWLEDGED') },
        ],
        pageInfo: { endCursor: 'acknowledged', hasNextPage: false },
      },
    })
    mocks.getCommand.mockImplementation(async (id: string) => command(
      id,
      id === OTHER_COMMAND_ID ? 'ACKNOWLEDGED' : 'DISPATCHED',
    ))

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Dispatched'))
    expect(screen.getByText('The broker accepted the publish. Device receipt is not confirmed yet.')).toBeTruthy()

    await fireEvent.click(document.querySelectorAll('.command-history-row')[1] as HTMLButtonElement)
    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Acknowledged'))
    expect(screen.getByText('The device reported receipt. Its final result is still pending.')).toBeTruthy()
    expect(document.querySelector('.command-current .command-status')?.textContent).not.toContain('Completed')
  })

  it('does not let an unrelated terminal history row resolve the saved intent', async () => {
    window.sessionStorage.setItem(`pulsegrid.command-intent.v1:${DEVICE_ID}`, JSON.stringify({
      version: 1,
      deviceId: DEVICE_ID,
      type: 'PING',
      idempotencyKey: IDEMPOTENCY_KEY,
      commandId: COMMAND_ID,
    }))
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [
          { cursor: 'terminal', node: command(OTHER_COMMAND_ID, 'COMPLETED') },
          { cursor: 'pending', node: command(COMMAND_ID) },
        ],
        pageInfo: { endCursor: 'pending', hasNextPage: false },
      },
    })
    mocks.getCommand.mockImplementation(async (id: string) => command(id, id === OTHER_COMMAND_ID ? 'COMPLETED' : 'PENDING'))

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: new RegExp(OTHER_COMMAND_ID) }))

    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Completed'))
    expect(screen.queryByRole('button', { name: 'Send PING' })).toBeNull()
    expect(readStoredRecord()).toMatchObject({ commandId: COMMAND_ID, idempotencyKey: IDEMPOTENCY_KEY })
  })

  it('keeps selected-command polls sequential and stops after the panel unmounts', async () => {
    vi.useFakeTimers()
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [{ cursor: 'pending', node: command() }],
        pageInfo: { endCursor: 'pending', hasNextPage: false },
      },
    })
    let finishSecondRead!: (value: CommandSnapshot) => void
    mocks.getCommand
      .mockResolvedValueOnce(command())
      .mockReturnValueOnce(new Promise((resolve) => { finishSecondRead = resolve }))

    const rendered = await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await Promise.resolve()
    await Promise.resolve()
    expect(mocks.getCommand).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1_999)
    expect(mocks.getCommand).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.getCommand).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(4_000)
    expect(mocks.getCommand).toHaveBeenCalledTimes(2)

    rendered.unmount()
    finishSecondRead(command())
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(mocks.getCommand).toHaveBeenCalledTimes(2)
  })

  it('stops automatic tracking at the bounded read count without inventing timeout', async () => {
    vi.useFakeTimers()
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [{ cursor: 'pending', node: command() }],
        pageInfo: { endCursor: 'pending', hasNextPage: false },
      },
    })
    mocks.getCommand.mockResolvedValue(command())

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(150_000)

    expect(mocks.getCommand).toHaveBeenCalledTimes(75)
    expect(screen.getByText(/Tracking paused — result not yet confirmed/)).toBeTruthy()
    expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Pending')
    await vi.advanceTimersByTimeAsync(5_000)
    expect(mocks.getCommand).toHaveBeenCalledTimes(75)
  })

  it('fails closed on corrupt recovery data and requires an explicit forget action', async () => {
    window.sessionStorage.setItem(`pulsegrid.command-intent.v1:${DEVICE_ID}`, '{')

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByText('Saved command recovery data is invalid.')).toBeTruthy())
    expect(screen.queryByRole('button', { name: 'Send PING' })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'Forget unusable recovery data' }))
    expect(screen.getByText(/review the recent command history first/i)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm clearing saved recovery data' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    expect(mocks.createCommand).not.toHaveBeenCalled()
  })
})

function readStoredRecord(): unknown {
  const record = window.sessionStorage.getItem(`pulsegrid.command-intent.v1:${DEVICE_ID}`)
  return record === null ? null : JSON.parse(record)
}
