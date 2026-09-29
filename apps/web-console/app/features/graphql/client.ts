export type GraphQLClientErrorCode
  = | 'BAD_USER_INPUT'
    | 'CONFLICT'
    | 'INTERNAL_SERVER_ERROR'
    | 'SERVICE_UNAVAILABLE'
    | 'UNKNOWN'

interface GraphQLErrorPayload {
  message?: unknown
  extensions?: unknown
}

interface GraphQLResponse<TData> {
  data?: TData
  errors?: GraphQLErrorPayload[]
}

export class GraphQLClientError extends Error {
  readonly code: GraphQLClientErrorCode
  readonly status: number | undefined

  constructor(message: string, code: GraphQLClientErrorCode = 'UNKNOWN', status?: number) {
    super(message)
    this.name = 'GraphQLClientError'
    this.code = code
    this.status = status
  }
}

const REQUEST_TIMEOUT_MS = 6_000

function errorCode(payload: GraphQLErrorPayload | undefined): GraphQLClientErrorCode {
  const extensions = payload?.extensions
  if (typeof extensions !== 'object' || extensions === null || !('code' in extensions)) return 'UNKNOWN'
  const code = extensions.code
  return code === 'BAD_USER_INPUT' || code === 'CONFLICT' || code === 'INTERNAL_SERVER_ERROR' || code === 'SERVICE_UNAVAILABLE'
    ? code
    : 'UNKNOWN'
}

function errorMessage(payload: GraphQLErrorPayload | undefined, serviceName: string): string {
  return typeof payload?.message === 'string' && payload.message !== ''
    ? payload.message
    : `${serviceName} request failed`
}

function serviceError(serviceName: string, detail: string, status?: number): GraphQLClientError {
  return new GraphQLClientError(`${serviceName} service ${detail}`, 'SERVICE_UNAVAILABLE', status)
}

function parseResponse<TData>(payload: unknown, status: number, serviceName: string): TData {
  if (typeof payload !== 'object' || payload === null) {
    throw serviceError(serviceName, 'returned an invalid response', status)
  }
  const response = payload as GraphQLResponse<TData>
  const firstError = Array.isArray(response.errors) ? response.errors[0] : undefined
  if (firstError !== undefined) {
    throw new GraphQLClientError(errorMessage(firstError, serviceName), errorCode(firstError), status)
  }
  if (!('data' in response) || response.data === undefined) {
    throw serviceError(serviceName, 'returned no data', status)
  }
  return response.data
}

export async function executeGraphQL<TData, TVariables extends Record<string, unknown>>(
  query: string,
  variables: TVariables,
  options: { signal?: AbortSignal, serviceName?: string } = {},
): Promise<TData> {
  const serviceName = options.serviceName ?? 'GraphQL'
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
      throw serviceError(serviceName, 'is unavailable')
    }

    let payload: unknown
    try {
      payload = await response.json()
    }
    catch {
      throw serviceError(serviceName, 'returned an invalid response', response.status)
    }
    return parseResponse<TData>(payload, response.status, serviceName)
  }
  finally {
    clearTimeout(timeout)
    options.signal?.removeEventListener('abort', abortExternal)
  }
}
