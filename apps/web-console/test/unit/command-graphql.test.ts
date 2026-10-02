import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createCommand,
  createCommandMutation,
  DEFAULT_COMMAND_PAGE_SIZE,
  deviceCommandsQuery,
  isTerminalCommand,
  listDeviceCommands,
  parseCommandSnapshot,
  type CommandSnapshot,
} from '../../app/features/commands/command-graphql'

const deviceID = '11111111-1111-4111-8111-111111111111'
const commandID = '22222222-2222-4222-8222-222222222222'
const key = '33333333-3333-4333-8333-333333333333'

function command(overrides: Partial<CommandSnapshot> = {}): CommandSnapshot {
  return {
    id: commandID,
    deviceId: deviceID,
    type: 'PING',
    status: 'PENDING',
    createdAt: '2026-10-02T10:00:00.000Z',
    updatedAt: '2026-10-02T10:00:00.000Z',
    expiresAt: '2026-10-02T10:02:00.000Z',
    dispatchedAt: null,
    acknowledgedAt: null,
    terminalAt: null,
    failureCode: null,
    ...overrides,
  }
}

function stubGraphQLResponse(data: unknown) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('command GraphQL boundary', () => {
  it('sends only a parameterless PING intent and parses the stored snapshot', async () => {
    const created = command({ status: 'COMPLETED', dispatchedAt: '2026-10-02T10:00:01.000Z', acknowledgedAt: '2026-10-02T10:00:02.000Z', terminalAt: '2026-10-02T10:00:03.000Z' })
    const fetchMock = stubGraphQLResponse({ createCommand: created })

    await expect(createCommand(deviceID, key)).resolves.toEqual(created)
    const request = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body)) as {
      query: string
      variables: { input: Record<string, unknown> }
    }
    expect(request.query).toBe(createCommandMutation)
    expect(request.variables.input).toEqual({ deviceId: deviceID, type: 'PING', idempotencyKey: key })
    expect(Object.keys(request.variables.input)).toEqual(['deviceId', 'type', 'idempotencyKey'])
  })

  it('rejects a command belonging to a different device', () => {
    expect(() => parseCommandSnapshot(command({ deviceId: '44444444-4444-4444-8444-444444444444' }), deviceID))
      .toThrow('Command response does not match the command contract')
  })

  it('rejects unknown statuses and inconsistent terminal or failure fields', () => {
    expect(() => parseCommandSnapshot(command({ status: 'UNKNOWN' as CommandSnapshot['status'] }), deviceID))
      .toThrow('Command response does not match the command contract')
    expect(() => parseCommandSnapshot(command({ status: 'COMPLETED' }), deviceID))
      .toThrow('Command response has inconsistent lifecycle fields')
    expect(() => parseCommandSnapshot(command({ status: 'FAILED', failureCode: null, terminalAt: '2026-10-02T10:00:03.000Z' }), deviceID))
      .toThrow('Command response has inconsistent lifecycle fields')
  })

  it('keeps ACK intermediate and accepts stored expiry only as a server status', () => {
    expect(parseCommandSnapshot(command({
      status: 'ACKNOWLEDGED',
      dispatchedAt: '2026-10-02T10:00:01Z',
      acknowledgedAt: '2026-10-02T10:00:02Z',
    }), deviceID).status).toBe('ACKNOWLEDGED')
    expect(parseCommandSnapshot(command({
      status: 'TIMED_OUT',
      terminalAt: '2026-10-02T10:02:01Z',
    }), deviceID).status).toBe('TIMED_OUT')
    expect(isTerminalCommand('ACKNOWLEDGED')).toBe(false)
    expect(isTerminalCommand('COMPLETED')).toBe(true)
  })

  it('loads the latest bounded history and validates its page cursor', async () => {
    const fetchMock = stubGraphQLResponse({
      deviceCommands: {
        edges: [{ cursor: 'cursor-1', node: command() }],
        pageInfo: { endCursor: 'cursor-1', hasNextPage: true },
      },
    })

    const result = await listDeviceCommands(deviceID)
    expect(result.deviceCommands.edges[0]?.node.id).toBe(commandID)
    expect(result.deviceCommands.pageInfo.hasNextPage).toBe(true)
    const request = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body)) as {
      query: string
      variables: { deviceId: string, first: number, after: null }
    }
    expect(request.query).toBe(deviceCommandsQuery)
    expect(request.variables).toEqual({ deviceId: deviceID, first: DEFAULT_COMMAND_PAGE_SIZE, after: null })
  })
})
