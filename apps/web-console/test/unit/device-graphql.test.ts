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
})
