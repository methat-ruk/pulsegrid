import { afterEach, describe, expect, it, vi } from 'vitest'

import type {
  DeviceGraphQLError } from '../../app/features/devices/device-graphql'
import {
  executeDeviceGraphQL,
  validateDeviceInput,
} from '../../app/features/devices/device-graphql'

afterEach(() => vi.restoreAllMocks())

describe('device GraphQL client', () => {
  it('matches repository Unicode whitespace and code-point validation', () => {
    expect(validateDeviceInput({ deviceKey: '\u00a0sensor-1', displayName: 'Temperature' })).toEqual({
      deviceKey: 'Enter a device key without leading or trailing whitespace.',
    })
    expect(validateDeviceInput({ deviceKey: 'sensor-1', displayName: '\u2003' })).toEqual({
      displayName: 'Enter a display name.',
    })
    expect(validateDeviceInput({ deviceKey: '😀'.repeat(129), displayName: 'Temperature' }).deviceKey).toContain('128')
    expect(validateDeviceInput({ deviceKey: 'sensor-1', displayName: '😀'.repeat(201) }).displayName).toContain('200')
    expect(validateDeviceInput({ deviceKey: '😀'.repeat(128), displayName: 'Temperature' })).toEqual({})
  })

  it('maps GraphQL errors and sends a fixed no-store browser request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      errors: [{ message: 'device key already exists', extensions: { code: 'CONFLICT' } }],
    }), {
      status: 200,
      headers: { 'content-type': 'application/graphql-response+json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(executeDeviceGraphQL('mutation Test { __typename }', {})).rejects.toMatchObject<DeviceGraphQLError>({
      code: 'CONFLICT',
      message: 'device key already exists',
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/graphql', expect.objectContaining({
      method: 'POST',
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      headers: {
        'accept': 'application/graphql-response+json',
        'content-type': 'application/json',
      },
    }))
  })

  it('sends the operation and variables without changing their shape', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      data: { device: null },
    }), {
      status: 200,
      headers: { 'content-type': 'application/graphql-response+json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(executeDeviceGraphQL('query Device($id: ID!) { device(id: $id) { id } }', { id: 'device-1' }))
      .resolves.toEqual({ device: null })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({
      query: 'query Device($id: ID!) { device(id: $id) { id } }',
      variables: { id: 'device-1' },
    })
  })

  it('maps malformed JSON responses to a safe service error', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{', {
      status: 200,
      headers: { 'content-type': 'application/graphql-response+json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(executeDeviceGraphQL('query Test { __typename }', {})).rejects.toMatchObject<DeviceGraphQLError>({
      code: 'SERVICE_UNAVAILABLE',
      status: 200,
    })
  })

  it('propagates an external abort to the browser request', async () => {
    const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })
    }))
    vi.stubGlobal('fetch', fetchMock)
    const controller = new AbortController()
    const request = executeDeviceGraphQL('query Test { __typename }', {}, { signal: controller.signal })

    controller.abort()

    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('aborts a request that exceeds the client timeout', async () => {
    vi.useFakeTimers()
    try {
      const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })
      }))
      vi.stubGlobal('fetch', fetchMock)
      const request = executeDeviceGraphQL('query Test { __typename }', {})
      const rejection = expect(request).rejects.toMatchObject({ name: 'AbortError' })

      await vi.advanceTimersByTimeAsync(6_000)

      await rejection
    }
    finally {
      vi.useRealTimers()
    }
  })
})
