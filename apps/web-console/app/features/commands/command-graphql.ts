import { executeGraphQL, GraphQLClientError } from '../graphql/client'

export { GraphQLClientError as CommandGraphQLError } from '../graphql/client'

export type CommandStatus
  = | 'PENDING'
    | 'DISPATCHED'
    | 'ACKNOWLEDGED'
    | 'COMPLETED'
    | 'FAILED'
    | 'TIMED_OUT'

export type CommandFailureCode = 'DEVICE_REPORTED_FAILURE' | 'DELIVERY_FAILED'

export interface CommandSnapshot {
  id: string
  deviceId: string
  type: 'PING'
  status: CommandStatus
  createdAt: string
  updatedAt: string
  expiresAt: string
  dispatchedAt: string | null
  acknowledgedAt: string | null
  terminalAt: string | null
  failureCode: CommandFailureCode | null
}

export interface CommandConnection {
  edges: Array<{ cursor: string, node: CommandSnapshot }>
  pageInfo: { endCursor: string | null, hasNextPage: boolean }
}

export interface CommandPage {
  deviceCommands: CommandConnection
}

export const DEFAULT_COMMAND_PAGE_SIZE = 20

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u
const TIMESTAMP_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/u

const COMMAND_FIELDS = `
  id
  deviceId
  type
  status
  createdAt
  updatedAt
  expiresAt
  dispatchedAt
  acknowledgedAt
  terminalAt
  failureCode
`

export const createCommandMutation = `mutation CreateCommand($input: CreateCommandInput!) {
  createCommand(input: $input) { ${COMMAND_FIELDS} }
}`

export const commandQuery = `query Command($id: ID!) {
  command(id: $id) { ${COMMAND_FIELDS} }
}`

export const deviceCommandsQuery = `query DeviceCommands($deviceId: ID!, $first: Int!, $after: String) {
  deviceCommands(deviceId: $deviceId, first: $first, after: $after) {
    edges {
      cursor
      node { ${COMMAND_FIELDS} }
    }
    pageInfo { endCursor hasNextPage }
  }
}`

export function isCanonicalUUID(value: unknown): value is string {
  return typeof value === 'string' && UUID_PATTERN.test(value) && value !== '00000000-0000-0000-0000-000000000000'
}

function isTimestamp(value: unknown): value is string {
  return typeof value === 'string' && TIMESTAMP_PATTERN.test(value) && Number.isFinite(Date.parse(value))
}

function nullableTimestamp(value: unknown): value is string | null {
  return value === null || isTimestamp(value)
}

function isCommandStatus(value: unknown): value is CommandStatus {
  return value === 'PENDING'
    || value === 'DISPATCHED'
    || value === 'ACKNOWLEDGED'
    || value === 'COMPLETED'
    || value === 'FAILED'
    || value === 'TIMED_OUT'
}

function isFailureCode(value: unknown): value is CommandFailureCode {
  return value === 'DEVICE_REPORTED_FAILURE' || value === 'DELIVERY_FAILED'
}

export function parseCommandSnapshot(value: unknown, expectedDeviceId: string): CommandSnapshot {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('Command response is not an object')
  }

  const record = value as Record<string, unknown>
  const id = record.id
  const deviceId = record.deviceId
  const status = record.status
  const createdAt = record.createdAt
  const updatedAt = record.updatedAt
  const expiresAt = record.expiresAt
  const dispatchedAt = record.dispatchedAt
  const acknowledgedAt = record.acknowledgedAt
  const terminalAt = record.terminalAt
  const failureCode = record.failureCode
  const terminal = status === 'COMPLETED' || status === 'FAILED' || status === 'TIMED_OUT'

  if (!isCanonicalUUID(id) || !isCanonicalUUID(deviceId) || deviceId !== expectedDeviceId
    || record.type !== 'PING' || !isCommandStatus(status)
    || !isTimestamp(createdAt) || !isTimestamp(updatedAt) || !isTimestamp(expiresAt)
    || !nullableTimestamp(dispatchedAt) || !nullableTimestamp(acknowledgedAt)
    || !nullableTimestamp(terminalAt)
    || !(failureCode === null || isFailureCode(failureCode))) {
    throw new Error('Command response does not match the command contract')
  }

  if (Date.parse(expiresAt) <= Date.parse(createdAt)
    || Date.parse(updatedAt) < Date.parse(createdAt)
    || terminal !== (terminalAt !== null)
    || (status === 'FAILED') !== (failureCode !== null)
    || (status === 'ACKNOWLEDGED' && acknowledgedAt === null)) {
    throw new Error('Command response has inconsistent lifecycle fields')
  }

  return {
    id,
    deviceId,
    type: 'PING',
    status,
    createdAt,
    updatedAt,
    expiresAt,
    dispatchedAt,
    acknowledgedAt,
    terminalAt,
    failureCode,
  }
}

export function isTerminalCommand(status: CommandStatus): boolean {
  return status === 'COMPLETED' || status === 'FAILED' || status === 'TIMED_OUT'
}

function assertCommandInput(deviceId: string, idempotencyKey: string) {
  if (!isCanonicalUUID(deviceId) || !isCanonicalUUID(idempotencyKey)) {
    throw new GraphQLClientError('Command input is invalid', 'BAD_USER_INPUT')
  }
}

export async function createCommand(deviceId: string, idempotencyKey: string, signal?: AbortSignal): Promise<CommandSnapshot> {
  assertCommandInput(deviceId, idempotencyKey)
  const response = await executeGraphQL<{ createCommand: unknown }, {
    input: { deviceId: string, type: 'PING', idempotencyKey: string }
  }>(createCommandMutation, { input: { deviceId, type: 'PING', idempotencyKey } }, { signal, serviceName: 'command' })
  return parseCommandSnapshot(response.createCommand, deviceId)
}

export async function getCommand(id: string, deviceId: string, signal?: AbortSignal): Promise<CommandSnapshot | null> {
  assertCommandInput(deviceId, id)
  const response = await executeGraphQL<{ command: unknown }, { id: string }>(
    commandQuery,
    { id },
    { signal, serviceName: 'command' },
  )
  return response.command === null ? null : parseCommandSnapshot(response.command, deviceId)
}

export async function listDeviceCommands(deviceId: string, signal?: AbortSignal): Promise<CommandPage> {
  if (!isCanonicalUUID(deviceId)) throw new GraphQLClientError('Command input is invalid', 'BAD_USER_INPUT')
  const response = await executeGraphQL<{ deviceCommands: unknown }, {
    deviceId: string
    first: number
    after: null
  }>(deviceCommandsQuery, { deviceId, first: DEFAULT_COMMAND_PAGE_SIZE, after: null }, { signal, serviceName: 'command' })

  if (typeof response.deviceCommands !== 'object' || response.deviceCommands === null || Array.isArray(response.deviceCommands)) {
    throw new Error('Command history response is not an object')
  }
  const connection = response.deviceCommands as Record<string, unknown>
  const edges = connection.edges
  const pageInfo = connection.pageInfo
  if (!Array.isArray(edges) || edges.length > DEFAULT_COMMAND_PAGE_SIZE
    || typeof pageInfo !== 'object' || pageInfo === null || Array.isArray(pageInfo)) {
    throw new Error('Command history response does not match the command contract')
  }

  const parsedEdges = edges.map((edge) => {
    if (typeof edge !== 'object' || edge === null || Array.isArray(edge)) throw new Error('Command history edge is invalid')
    const record = edge as Record<string, unknown>
    if (typeof record.cursor !== 'string' || record.cursor.length === 0) throw new Error('Command history cursor is invalid')
    return { cursor: record.cursor, node: parseCommandSnapshot(record.node, deviceId) }
  })
  const page = pageInfo as Record<string, unknown>
  if (!(page.endCursor === null || (typeof page.endCursor === 'string' && page.endCursor.length > 0))
    || typeof page.hasNextPage !== 'boolean'
    || (parsedEdges.length > 0 && page.endCursor !== parsedEdges.at(-1)?.cursor)) {
    throw new Error('Command history page information is invalid')
  }

  return {
    deviceCommands: {
      edges: parsedEdges,
      pageInfo: { endCursor: page.endCursor, hasNextPage: page.hasNextPage },
    },
  }
}
