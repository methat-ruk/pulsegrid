import { fireEvent, screen, waitFor, cleanup } from '@testing-library/vue'
import { renderSuspended } from '@nuxt/test-utils/runtime'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DeviceCommandPanel from '../../app/components/DeviceCommandPanel.vue'
import type { CommandSnapshot } from '../../app/features/commands/command-graphql'

const DEVICE_ID = '11111111-1111-4111-8111-111111111111'
const OTHER_DEVICE_ID = '55555555-5555-4555-8555-555555555555'
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

function command(id = COMMAND_ID, status: CommandSnapshot['status'] = 'PENDING', deviceId = DEVICE_ID): CommandSnapshot {
  const terminal = status === 'COMPLETED' || status === 'FAILED' || status === 'TIMED_OUT'
  const timestamp = status === 'PENDING' ? '2026-10-02T10:00:00.000Z' : '2026-10-02T10:00:02.000Z'
  return {
    id,
    deviceId,
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
  let storageWriteCount = 0
  let failStorageWriteAt: number | null = null
  let storageRemoveFailures = 0

  beforeEach(() => {
    vi.resetAllMocks()
    storageWriteCount = 0
    failStorageWriteAt = null
    storageRemoveFailures = 0
    const data = new Map<string, string>()
    const storage: Storage = {
      getItem: key => data.get(key) ?? null,
      setItem: (key, value) => {
        storageWriteCount += 1
        if (storageWriteCount === failStorageWriteAt) throw new Error('storage write failed')
        data.set(key, String(value))
      },
      removeItem: (key) => {
        if (storageRemoveFailures > 0) {
          storageRemoveFailures -= 1
          throw new Error('storage cleanup failed')
        }
        data.delete(key)
      },
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

  it('shows a retry for a known recovered command when its direct lookup fails outside the latest page', async () => {
    window.sessionStorage.setItem(`pulsegrid.command-intent.v1:${DEVICE_ID}`, JSON.stringify({
      version: 1,
      deviceId: DEVICE_ID,
      type: 'PING',
      idempotencyKey: IDEMPOTENCY_KEY,
      commandId: COMMAND_ID,
    }))
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [{ cursor: 'recent', node: command(OTHER_COMMAND_ID, 'COMPLETED') }],
        pageInfo: { endCursor: 'recent', hasNextPage: true },
      },
    })
    mocks.getCommand
      .mockRejectedValueOnce(new Error('temporary read failure'))
      .mockResolvedValueOnce(command(COMMAND_ID))

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByText('The command status could not be refreshed. The last confirmed status remains visible.')).toBeTruthy())
    expect(screen.getByRole('button', { name: 'Retry command lookup' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: new RegExp(COMMAND_ID) })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'Retry command lookup' }))
    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Pending'))
    expect(mocks.getCommand).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('button', { name: 'Resume status tracking' })).toBeTruthy()
  })

  it('keeps a directly recovered command visible when initial history fails', async () => {
    window.sessionStorage.setItem(`pulsegrid.command-intent.v1:${DEVICE_ID}`, JSON.stringify({
      version: 1,
      deviceId: DEVICE_ID,
      type: 'PING',
      idempotencyKey: IDEMPOTENCY_KEY,
      commandId: COMMAND_ID,
    }))
    mocks.listDeviceCommands.mockRejectedValue(new Error('history service unavailable'))
    mocks.getCommand.mockResolvedValue(command(COMMAND_ID, 'COMPLETED'))

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })

    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Completed'))
    expect(screen.getByRole('alert').textContent).toContain('history')
    expect(document.querySelector('.command-current code.command-id')?.textContent).toBe(COMMAND_ID)
    expect(document.querySelector('.command-history')).toBeNull()
  })

  it('does not show terminal cleanup confirmation when saving a pending command ID fails', async () => {
    failStorageWriteAt = 2
    mocks.createCommand.mockResolvedValue(command(COMMAND_ID, 'PENDING'))
    mocks.getCommand.mockResolvedValue(command(COMMAND_ID, 'PENDING'))
    vi.spyOn(window.crypto, 'randomUUID').mockReturnValue(IDEMPOTENCY_KEY)

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm and send PING' }))

    await waitFor(() => expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Pending'))
    expect(screen.getByRole('alert').textContent).toContain('recovery record could not be updated')
    expect(screen.queryByRole('button', { name: 'Retry recovery cleanup' })).toBeNull()
    expect(screen.queryByText(/The terminal result is stored by the server/i)).toBeNull()
    expect(readStoredRecord()).toMatchObject({ idempotencyKey: IDEMPOTENCY_KEY, commandId: null })
  })

  it('shows terminal cleanup recovery only after a terminal server result is known', async () => {
    storageRemoveFailures = 1
    mocks.createCommand.mockResolvedValue(command(COMMAND_ID, 'COMPLETED'))
    vi.spyOn(window.crypto, 'randomUUID').mockReturnValue(IDEMPOTENCY_KEY)

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'Send PING' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm and send PING' }))

    const retryCleanup = await screen.findByRole('button', { name: 'Retry recovery cleanup' })
    expect(screen.getAllByRole('alert').map(alert => alert.textContent).join(' ')).toContain('The terminal result is stored by the server')
    await fireEvent.click(retryCleanup)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send PING' })).toBeTruthy())
    expect(readStoredRecord()).toBeNull()
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

  it('pauses automatic status reads while hidden and resumes after the page becomes visible', async () => {
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [{ cursor: 'pending', node: command() }],
        pageInfo: { endCursor: 'pending', hasNextPage: false },
      },
    })
    mocks.getCommand.mockResolvedValue(command())

    await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(mocks.getCommand).toHaveBeenCalledTimes(1))
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    await new Promise(resolve => setTimeout(resolve, 2_100))
    expect(mocks.getCommand).toHaveBeenCalledTimes(1)

    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await waitFor(() => expect(mocks.getCommand).toHaveBeenCalledTimes(2))
    expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Pending')
  })

  it('ignores an older selected-command read after the operator selects another command', async () => {
    mocks.listDeviceCommands.mockResolvedValue({
      deviceCommands: {
        edges: [
          { cursor: 'current', node: command(COMMAND_ID) },
          { cursor: 'other', node: command(OTHER_COMMAND_ID) },
        ],
        pageInfo: { endCursor: 'other', hasNextPage: false },
      },
    })
    let finishOldRead!: (snapshot: CommandSnapshot) => void
    let finishNewRead!: (snapshot: CommandSnapshot) => void
    mocks.getCommand.mockImplementation((id: string) => new Promise((resolve) => {
      if (id === COMMAND_ID) finishOldRead = resolve
      else finishNewRead = resolve
    }))

    const rendered = await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(mocks.getCommand).toHaveBeenCalledTimes(1))
    await fireEvent.click(screen.getByRole('button', { name: new RegExp(OTHER_COMMAND_ID) }))
    await waitFor(() => expect(mocks.getCommand).toHaveBeenCalledTimes(2))

    finishOldRead(command(COMMAND_ID, 'COMPLETED'))
    await Promise.resolve()
    expect(document.querySelector('.command-current code.command-id')?.textContent).toBe(OTHER_COMMAND_ID)
    expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Pending')

    rendered.unmount()
    finishNewRead(command(OTHER_COMMAND_ID, 'COMPLETED'))
  })

  it('ignores a pending command read from the previous device after device selection changes', async () => {
    mocks.listDeviceCommands.mockImplementation(async (deviceId: string) => ({
      deviceCommands: {
        edges: [{
          cursor: deviceId === DEVICE_ID ? 'old-device' : 'new-device',
          node: deviceId === DEVICE_ID
            ? command(COMMAND_ID, 'PENDING', DEVICE_ID)
            : command(OTHER_COMMAND_ID, 'COMPLETED', OTHER_DEVICE_ID),
        }],
        pageInfo: { endCursor: deviceId === DEVICE_ID ? 'old-device' : 'new-device', hasNextPage: false },
      },
    }))
    let finishOldRead!: (snapshot: CommandSnapshot) => void
    mocks.getCommand.mockImplementation((id: string, deviceId: string) => {
      if (deviceId === DEVICE_ID) {
        return new Promise((resolve) => {
          finishOldRead = resolve
        })
      }
      return Promise.resolve(command(id, 'COMPLETED', OTHER_DEVICE_ID))
    })

    const rendered = await renderSuspended(DeviceCommandPanel, { props: { deviceId: DEVICE_ID, deviceKey: 'device-demo' } })
    await waitFor(() => expect(mocks.getCommand).toHaveBeenCalledTimes(1))
    await rendered.rerender({ deviceId: OTHER_DEVICE_ID, deviceKey: 'other-device' })
    await waitFor(() => expect(document.querySelector('.command-current code.command-id')?.textContent).toBe(OTHER_COMMAND_ID))

    finishOldRead(command(COMMAND_ID, 'COMPLETED', DEVICE_ID))
    await Promise.resolve()
    expect(document.querySelector('.command-current code.command-id')?.textContent).toBe(OTHER_COMMAND_ID)
    expect(document.querySelector('.command-current .command-status')?.textContent).toContain('Completed')
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
