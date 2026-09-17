import type { IncomingMessage, ServerResponse } from 'node:http'

export const GRAPHQL_REQUEST_PATH = '/graphql'
export const GRAPHQL_RESPONSE_MEDIA_TYPE = 'application/graphql-response+json'
export const MAX_GRAPHQL_REQUEST_BYTES = 64 * 1024
export const MAX_GRAPHQL_RESPONSE_BYTES = 256 * 1024
export const GRAPHQL_UPSTREAM_TIMEOUT_MS = 5_000

const SERVICE_UNAVAILABLE_MESSAGE = 'device service is unavailable'
const REQUEST_CONTENT_TYPE_MESSAGE = 'request content type must be application/json'
const REQUEST_TOO_LARGE_MESSAGE = 'request body is too large'

type GraphqlErrorCode = 'SERVICE_UNAVAILABLE' | 'BAD_USER_INPUT'

export type GraphqlProxyEvent = {
  node: {
    req: IncomingMessage
    res: ServerResponse
  }
}

function mediaType(value: string | undefined): string {
  return value?.split(';', 1)[0]?.trim().toLowerCase() ?? ''
}

function errorBody(message: string, code: GraphqlErrorCode): string {
  return JSON.stringify({
    errors: [{ message, extensions: { code } }],
  })
}

async function sendGraphqlBody(event: GraphqlProxyEvent, status: number, body: Uint8Array | string, contentType = GRAPHQL_RESPONSE_MEDIA_TYPE): Promise<void> {
  event.node.res.statusCode = status
  event.node.res.setHeader('cache-control', 'no-store')
  event.node.res.setHeader('content-type', contentType)
  event.node.res.end(typeof body === 'string' ? body : Buffer.from(body))
}

export async function sendGraphqlUnavailable(event: GraphqlProxyEvent): Promise<void> {
  await sendGraphqlBody(event, 503, errorBody(SERVICE_UNAVAILABLE_MESSAGE, 'SERVICE_UNAVAILABLE'))
}

async function readRequestBody(event: GraphqlProxyEvent): Promise<Uint8Array | 'too-large'> {
  const contentLength = event.node.req.headers['content-length']
  if (typeof contentLength === 'string') {
    const declaredLength = Number(contentLength)
    if (Number.isFinite(declaredLength) && declaredLength > MAX_GRAPHQL_REQUEST_BYTES) {
      return 'too-large'
    }
  }

  const chunks: Buffer[] = []
  let total = 0
  for await (const chunk of event.node.req) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)
    total += buffer.byteLength
    if (total > MAX_GRAPHQL_REQUEST_BYTES) return 'too-large'
    chunks.push(buffer)
  }
  return Buffer.concat(chunks, total)
}

async function readResponseBody(response: Response): Promise<Uint8Array | undefined> {
  const contentLength = response.headers.get('content-length')
  if (contentLength !== null) {
    const declaredLength = Number(contentLength)
    if (Number.isFinite(declaredLength) && declaredLength > MAX_GRAPHQL_RESPONSE_BYTES) {
      return undefined
    }
  }
  if (response.body === null) return new Uint8Array()

  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      total += value.byteLength
      if (total > MAX_GRAPHQL_RESPONSE_BYTES) {
        await reader.cancel()
        return undefined
      }
      chunks.push(value)
    }
  }
  finally {
    reader.releaseLock()
  }

  const body = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    body.set(chunk, offset)
    offset += chunk.byteLength
  }
  return body
}

export async function proxyGraphqlRequest(event: GraphqlProxyEvent, backendOrigin: string | undefined): Promise<void> {
  if (!backendOrigin) {
    await sendGraphqlUnavailable(event)
    return
  }

  const requestContentType = mediaType(event.node.req.headers['content-type'])
  if (requestContentType !== 'application/json') {
    await sendGraphqlBody(event, 415, errorBody(REQUEST_CONTENT_TYPE_MESSAGE, 'BAD_USER_INPUT'))
    return
  }

  const requestBody = await readRequestBody(event)
  if (requestBody === 'too-large') {
    await sendGraphqlBody(event, 413, errorBody(REQUEST_TOO_LARGE_MESSAGE, 'BAD_USER_INPUT'))
    return
  }

  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), GRAPHQL_UPSTREAM_TIMEOUT_MS)
  try {
    const response = await fetch(`${backendOrigin}${GRAPHQL_REQUEST_PATH}`, {
      method: 'POST',
      headers: {
        'accept': GRAPHQL_RESPONSE_MEDIA_TYPE,
        'content-type': 'application/json',
      },
      body: requestBody as unknown as BodyInit,
      redirect: 'manual',
      signal: controller.signal,
    })
    if (response.status >= 300 && response.status < 400) {
      await sendGraphqlUnavailable(event)
      return
    }
    const responseContentType = response.headers.get('content-type')
    if (mediaType(responseContentType ?? undefined) !== GRAPHQL_RESPONSE_MEDIA_TYPE) {
      await sendGraphqlUnavailable(event)
      return
    }
    const responseBody = await readResponseBody(response)
    if (responseBody === undefined) {
      await sendGraphqlUnavailable(event)
      return
    }
    await sendGraphqlBody(event, response.status, responseBody, responseContentType ?? GRAPHQL_RESPONSE_MEDIA_TYPE)
  }
  catch {
    await sendGraphqlUnavailable(event)
  }
  finally {
    clearTimeout(timeout)
  }
}
