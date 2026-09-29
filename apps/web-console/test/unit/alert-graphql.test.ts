import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  alertQuery,
  getAlert,
  listAlerts,
} from '../../app/features/alerts/alert-graphql'
import { GraphQLClientError } from '../../app/features/graphql/client'

afterEach(() => vi.restoreAllMocks())

describe('alert GraphQL client', () => {
  it('lists only the alert row fields and passes the opaque cursor and device scope unchanged', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      data: { alerts: { edges: [], pageInfo: { endCursor: null, hasNextPage: false } } },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listAlerts(50, 'opaque-cursor', 'device-id')).resolves.toEqual({
      alerts: { edges: [], pageInfo: { endCursor: null, hasNextPage: false } },
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string) as { query: string, variables: Record<string, unknown> }
    expect(body.query).toContain('query Alerts')
    expect(body.query).toContain('comparator')
    expect(body.query).not.toContain('messageId')
    expect(body.query).not.toContain('deviceTelemetry')
    expect(body.variables).toEqual({ first: 50, after: 'opaque-cursor', deviceId: 'device-id' })
  })

  it('loads the complete stored snapshot without fetching current telemetry or rules', async () => {
    const alert = {
      id: 'alert-id',
      deviceId: 'device-id',
      ruleId: 'rule-id',
      messageId: 'message-id',
      observedAt: '2026-09-24T10:00:00Z',
      receivedAt: '2026-09-24T10:00:01Z',
      temperatureCelsius: 31,
      metric: 'TEMPERATURE_CELSIUS',
      comparator: 'GT',
      thresholdCelsius: 25,
      createdAt: '2026-09-24T10:00:01Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { alert } }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAlert('alert-id')).resolves.toEqual({ alert })
    expect(alertQuery).toContain('query Alert')
    expect(alertQuery).not.toContain('deviceTelemetry')
    expect(alertQuery).not.toContain('thresholdRules')
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string) as { variables: Record<string, unknown> }
    expect(body.variables).toEqual({ id: 'alert-id' })
  })

  it('maps API errors to the shared safe error type', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({
      errors: [{ message: 'invalid cursor', extensions: { code: 'BAD_USER_INPUT' } }],
    }), { status: 200 }))))

    await expect(listAlerts()).rejects.toBeInstanceOf(GraphQLClientError)
    await expect(listAlerts()).rejects.toMatchObject({ code: 'BAD_USER_INPUT', message: 'invalid cursor' })
  })
})
