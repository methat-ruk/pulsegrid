// @vitest-environment node

import { createServer, type IncomingMessage, type RequestListener, type Server } from 'node:http'

import { afterEach, describe, expect, it } from 'vitest'

import { proxyGraphqlRequest, type GraphqlProxyEvent } from '../../server/utils/graphql-proxy'

const servers: Server[] = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(server => new Promise<void>(resolve => server.close(() => resolve()))))
})

async function startServer(handler: RequestListener): Promise<string> {
  const server = createServer(handler)
  servers.push(server)
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', () => resolve()))
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('test server did not expose an address')
  return `http://127.0.0.1:${address.port}`
}

async function startAdapter(backendOrigin: string | undefined): Promise<string> {
  return startServer(async (request, response) => {
    if (request.url !== '/api/graphql') {
      response.statusCode = 404
      response.end()
      return
    }
    await proxyGraphqlRequest({ node: { req: request, res: response } } satisfies GraphqlProxyEvent, backendOrigin)
  })
}

async function request(origin: string, body = '{"query":"{ __typename }"}', contentType = 'application/json') {
  return fetch(`${origin}/api/graphql`, {
    method: 'POST',
    headers: { 'content-type': contentType },
    body,
  })
}

function collectRequest(request: IncomingMessage): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = []
    request.on('data', chunk => chunks.push(Buffer.from(chunk)))
    request.on('end', () => resolve(Buffer.concat(chunks)))
    request.on('error', reject)
  })
}

describe('GraphQL same-origin adapter', () => {
  it('forwards only the fixed GraphQL request and preserves the upstream response', async () => {
    let receivedHeaders: IncomingMessage['headers'] | undefined
    let receivedBody: Buffer | undefined
    const backend = await startServer(async (incoming, response) => {
      receivedHeaders = incoming.headers
      receivedBody = await collectRequest(incoming)
      response.statusCode = 422
      response.setHeader('content-type', 'application/graphql-response+json; charset=utf-8')
      response.end('{"errors":[{"message":"bad input"}]}')
    })
    const adapter = await startAdapter(backend)

    const response = await fetch(`${adapter}/api/graphql`, {
      method: 'POST',
      headers: {
        'content-type': 'application/json; charset=utf-8',
        'cookie': 'session=should-not-forward',
        'authorization': 'Bearer should-not-forward',
        'x-forwarded-for': 'should-not-forward',
      },
      body: '{"query":"query Device { __typename }"}',
    })

    expect(response.status).toBe(422)
    expect(response.headers.get('content-type')).toContain('application/graphql-response+json')
    expect(response.headers.get('cache-control')).toBe('no-store')
    expect(await response.text()).toBe('{"errors":[{"message":"bad input"}]}')
    expect(receivedBody?.toString()).toBe('{"query":"query Device { __typename }"}')
    expect(receivedHeaders?.accept).toBe('application/graphql-response+json')
    expect(receivedHeaders?.cookie).toBeUndefined()
    expect(receivedHeaders?.authorization).toBeUndefined()
    expect(receivedHeaders?.['x-forwarded-for']).toBeUndefined()
  })

  it('returns a safe unavailable envelope when the backend origin is absent', async () => {
    const adapter = await startAdapter(undefined)
    const response = await request(adapter)

    expect(response.status).toBe(503)
    expect(response.headers.get('content-type')).toContain('application/graphql-response+json')
    expect(await response.json()).toEqual({
      errors: [{ message: 'device service is unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }],
    })
  })

  it('rejects unsupported browser media types before contacting the backend', async () => {
    let contacted = false
    const backend = await startServer((_request, response) => {
      contacted = true
      response.end()
    })
    const adapter = await startAdapter(backend)
    const response = await request(adapter, '{}', 'text/plain')

    expect(response.status).toBe(415)
    expect(contacted).toBe(false)
    expect(await response.json()).toEqual({
      errors: [{ message: 'request content type must be application/json', extensions: { code: 'BAD_USER_INPUT' } }],
    })
  })

  it('bounds chunked request bodies', async () => {
    let contacted = false
    const backend = await startServer((_request, response) => {
      contacted = true
      response.end()
    })
    const adapter = await startAdapter(backend)
    const response = await request(adapter, 'x'.repeat(64 * 1024 + 1))

    expect(response.status).toBe(413)
    expect(contacted).toBe(false)
    expect(await response.json()).toEqual({
      errors: [{ message: 'request body is too large', extensions: { code: 'BAD_USER_INPUT' } }],
    })
  })

  it('maps an invalid upstream response media type to the safe unavailable envelope', async () => {
    const backend = await startServer((_request, response) => {
      response.setHeader('content-type', 'application/json')
      response.end('{"data":{}}')
    })
    const adapter = await startAdapter(backend)
    const response = await request(adapter)

    expect(response.status).toBe(503)
    expect(await response.json()).toEqual({
      errors: [{ message: 'device service is unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }],
    })
  })

  it('bounds upstream response bodies', async () => {
    const backend = await startServer((_request, response) => {
      response.setHeader('content-type', 'application/graphql-response+json')
      response.end('x'.repeat(256 * 1024 + 1))
    })
    const adapter = await startAdapter(backend)
    const response = await request(adapter)

    expect(response.status).toBe(503)
    expect(await response.json()).toEqual({
      errors: [{ message: 'device service is unavailable', extensions: { code: 'SERVICE_UNAVAILABLE' } }],
    })
  })
})
