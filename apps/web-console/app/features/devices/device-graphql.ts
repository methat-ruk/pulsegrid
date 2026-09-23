export interface Device {
  id: string
  deviceKey: string
  displayName: string
  createdAt: string
}

export interface DeviceEdge {
  cursor: string
  node: Device
}

export interface DevicePageInfo {
  endCursor: string | null
  hasNextPage: boolean
}

export interface DeviceConnection {
  edges: DeviceEdge[]
  pageInfo: DevicePageInfo
}

export interface DeviceCurrentState {
  messageId: string
  observedAt: string
  receivedAt: string
  temperatureCelsius: number
  lastSeenAt: string
}

export interface TelemetryPoint {
  messageId: string
  observedAt: string
  receivedAt: string
  temperatureCelsius: number
}

export interface TelemetryEdge {
  cursor: string
  node: TelemetryPoint
}

export interface TelemetryConnection {
  edges: TelemetryEdge[]
  pageInfo: DevicePageInfo
}

export interface DeviceTelemetryOverview {
  deviceCurrentState: DeviceCurrentState | null
  deviceTelemetry: TelemetryConnection
}

interface GraphqlErrorPayload {
  message?: unknown
  extensions?: unknown
}

interface GraphqlResponse<TData> {
  data?: TData
  errors?: GraphqlErrorPayload[]
}

export type DeviceErrorCode
  = | 'BAD_USER_INPUT'
    | 'CONFLICT'
    | 'INTERNAL_SERVER_ERROR'
    | 'SERVICE_UNAVAILABLE'
    | 'UNKNOWN'

export class DeviceGraphQLError extends Error {
  readonly code: DeviceErrorCode
  readonly status: number | undefined

  constructor(message: string, code: DeviceErrorCode = 'UNKNOWN', status?: number) {
    super(message)
    this.name = 'DeviceGraphQLError'
    this.code = code
    this.status = status
  }
}

const REQUEST_TIMEOUT_MS = 6_000
const REPOSITORY_WHITESPACE = /^\p{White_Space}+|\p{White_Space}+$/gu

function trimRepositoryWhitespace(value: string): string {
  return value.replace(REPOSITORY_WHITESPACE, '')
}

function errorCode(payload: GraphqlErrorPayload | undefined): DeviceErrorCode {
  const extensions = payload?.extensions
  if (typeof extensions !== 'object' || extensions === null || !('code' in extensions)) return 'UNKNOWN'
  const code = extensions.code
  return code === 'BAD_USER_INPUT' || code === 'CONFLICT' || code === 'INTERNAL_SERVER_ERROR' || code === 'SERVICE_UNAVAILABLE'
    ? code
    : 'UNKNOWN'
}

function errorMessage(payload: GraphqlErrorPayload | undefined): string {
  return typeof payload?.message === 'string' && payload.message !== ''
    ? payload.message
    : 'device request failed'
}

function parseResponse<TData>(payload: unknown, status: number): TData {
  if (typeof payload !== 'object' || payload === null) {
    throw new DeviceGraphQLError('device service returned an invalid response', 'SERVICE_UNAVAILABLE', status)
  }
  const response = payload as GraphqlResponse<TData>
  const firstError = Array.isArray(response.errors) ? response.errors[0] : undefined
  if (firstError !== undefined) {
    throw new DeviceGraphQLError(errorMessage(firstError), errorCode(firstError), status)
  }
  if (!('data' in response) || response.data === undefined) {
    throw new DeviceGraphQLError('device service returned no data', 'SERVICE_UNAVAILABLE', status)
  }
  return response.data
}

export async function executeDeviceGraphQL<TData, TVariables extends Record<string, unknown>>(
  query: string,
  variables: TVariables,
  options: { signal?: AbortSignal } = {},
): Promise<TData> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
  const abortExternal = () => controller.abort(options.signal?.reason)
  options.signal?.addEventListener('abort', abortExternal, { once: true })

  try {
    let response: Response
    try {
      response = await fetch('/api/graphql', {
        method: 'POST',
        headers: {
          'accept': 'application/graphql-response+json',
          'content-type': 'application/json',
        },
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error',
        body: JSON.stringify({ query, variables }),
        signal: controller.signal,
      })
    }
    catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error
      throw new DeviceGraphQLError('device service is unavailable', 'SERVICE_UNAVAILABLE')
    }

    let payload: unknown
    try {
      payload = await response.json()
    }
    catch {
      throw new DeviceGraphQLError('device service returned an invalid response', 'SERVICE_UNAVAILABLE', response.status)
    }
    return parseResponse<TData>(payload, response.status)
  }
  finally {
    clearTimeout(timeout)
    options.signal?.removeEventListener('abort', abortExternal)
  }
}

export const listDevicesQuery = `query ListDevices($first: Int!, $after: String) {
  devices(first: $first, after: $after) {
    edges {
      cursor
      node {
        id
        deviceKey
        displayName
        createdAt
      }
    }
    pageInfo {
      endCursor
      hasNextPage
    }
  }
}`

export const deviceQuery = `query Device($id: ID!) {
  device(id: $id) {
    id
    deviceKey
    displayName
    createdAt
  }
}`

export const createDeviceMutation = `mutation CreateDevice($input: CreateDeviceInput!) {
  createDevice(input: $input) {
    id
    deviceKey
    displayName
    createdAt
  }
}`

export const deviceTelemetryOverviewQuery = `query DeviceTelemetryOverview($deviceId: ID!, $first: Int!, $after: String) {
  deviceCurrentState(deviceId: $deviceId) {
    messageId
    observedAt
    receivedAt
    temperatureCelsius
    lastSeenAt
  }
  deviceTelemetry(deviceId: $deviceId, first: $first, after: $after) {
    edges {
      cursor
      node {
        messageId
        observedAt
        receivedAt
        temperatureCelsius
      }
    }
    pageInfo {
      endCursor
      hasNextPage
    }
  }
}`

export const deviceTelemetryPageQuery = `query DeviceTelemetryPage($deviceId: ID!, $first: Int!, $after: String) {
  deviceTelemetry(deviceId: $deviceId, first: $first, after: $after) {
    edges {
      cursor
      node {
        messageId
        observedAt
        receivedAt
        temperatureCelsius
      }
    }
    pageInfo {
      endCursor
      hasNextPage
    }
  }
}`

export function listDevices(first: number, after: string | null, signal?: AbortSignal) {
  return executeDeviceGraphQL<{ devices: DeviceConnection }, { first: number, after: string | null }>(
    listDevicesQuery,
    { first, after },
    { signal },
  )
}

export function getDevice(id: string, signal?: AbortSignal) {
  return executeDeviceGraphQL<{ device: Device | null }, { id: string }>(deviceQuery, { id }, { signal })
}

export function getDeviceTelemetryOverview(deviceId: string, first = 50, after: string | null = null, signal?: AbortSignal) {
  return executeDeviceGraphQL<DeviceTelemetryOverview, { deviceId: string, first: number, after: string | null }>(
    deviceTelemetryOverviewQuery,
    { deviceId, first, after },
    { signal },
  )
}

export function getDeviceTelemetryPage(deviceId: string, first = 50, after: string | null, signal?: AbortSignal) {
  return executeDeviceGraphQL<{ deviceTelemetry: TelemetryConnection }, { deviceId: string, first: number, after: string | null }>(
    deviceTelemetryPageQuery,
    { deviceId, first, after },
    { signal },
  )
}

export function createDevice(input: { deviceKey: string, displayName: string }, signal?: AbortSignal) {
  return executeDeviceGraphQL<{ createDevice: Device }, { input: typeof input }>(
    createDeviceMutation,
    { input },
    { signal },
  )
}

export function validateDeviceInput(input: { deviceKey: string, displayName: string }): Partial<Record<'deviceKey' | 'displayName', string>> {
  const errors: Partial<Record<'deviceKey' | 'displayName', string>> = {}
  if (input.deviceKey.length === 0 || input.deviceKey !== trimRepositoryWhitespace(input.deviceKey)) {
    errors.deviceKey = 'Enter a device key without leading or trailing whitespace.'
  }
  else if ([...input.deviceKey].length > 128) {
    errors.deviceKey = 'Device key must be 128 characters or fewer.'
  }

  const displayNameLength = [...input.displayName].length
  if (displayNameLength === 0 || trimRepositoryWhitespace(input.displayName).length === 0) {
    errors.displayName = 'Enter a display name.'
  }
  else if (displayNameLength > 200) {
    errors.displayName = 'Display name must be 200 characters or fewer.'
  }
  return errors
}
