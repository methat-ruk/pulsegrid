// @vitest-environment node

import { createServer, type RequestListener, type Server } from 'node:http'

import { afterEach, describe, expect, it } from 'vitest'

import { fetchBackendReadiness } from '../../server/utils/backend-readiness'

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

describe('fetchBackendReadiness', () => {
  it('accepts only the exact ready response', async () => {
    const origin = await startServer((_request, response) => {
      response.setHeader('content-type', 'application/json')
      response.end('{"status":"ready"}')
    })

    await expect(fetchBackendReadiness(origin)).resolves.toEqual({ status: 'ready' })
  })

  it.each([
    { name: 'not ready', statusCode: 503, body: '{"status":"not_ready","reason":"starting"}' },
    { name: 'malformed', statusCode: 200, body: '{"status":' },
    { name: 'unexpected body', statusCode: 200, body: '{"status":"ok"}' },
    { name: 'redirect', statusCode: 302, body: '' },
  ])('maps $name to unavailable', async ({ statusCode, body }) => {
    const origin = await startServer((_request, response) => {
      response.statusCode = statusCode
      response.setHeader('location', '/other')
      response.end(body)
    })

    await expect(fetchBackendReadiness(origin)).resolves.toEqual({ status: 'unavailable' })
  })

  it('maps a refused connection to unavailable', async () => {
    await expect(fetchBackendReadiness('http://127.0.0.1:1')).resolves.toEqual({ status: 'unavailable' })
  })
})
