import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  createCommandIntent,
  forgetUnusableCommandIntent,
  readCommandIntent,
  removeCommandIntent,
  writeCommandIntent,
} from '../../app/features/commands/command-recovery'

const deviceID = '11111111-1111-4111-8111-111111111111'
const key = '33333333-3333-4333-8333-333333333333'
const commandID = '22222222-2222-4222-8222-222222222222'

beforeEach(() => {
  sessionStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('same-tab command recovery metadata', () => {
  it('round-trips only the device-scoped intent fields', () => {
    const intent = createCommandIntent(deviceID, key)
    expect(intent).toEqual({ deviceId: deviceID, type: 'PING', idempotencyKey: key, commandId: null })
    expect(intent).not.toBeNull()
    expect(writeCommandIntent(intent!)).toBe(true)
    expect(readCommandIntent(deviceID)).toEqual({ kind: 'valid', intent })

    const updated = { ...intent!, commandId: commandID }
    expect(writeCommandIntent(updated)).toBe(true)
    expect(readCommandIntent(deviceID)).toEqual({ kind: 'valid', intent: updated })
    expect(sessionStorage.getItem(`pulsegrid.command-intent.v1:${deviceID}`)).not.toContain('organization')
  })

  it('rejects a corrupt or mismatched record without replacing its identity', () => {
    const storageKey = `pulsegrid.command-intent.v1:${deviceID}`
    sessionStorage.setItem(storageKey, JSON.stringify({
      version: 1,
      deviceId: '44444444-4444-4444-8444-444444444444',
      type: 'PING',
      idempotencyKey: key,
      commandId: null,
    }))

    expect(readCommandIntent(deviceID)).toEqual({ kind: 'invalid' })
    expect(writeCommandIntent(createCommandIntent(deviceID, '55555555-5555-4555-8555-555555555555')!)).toBe(true)
    expect(readCommandIntent(deviceID)).toMatchObject({ kind: 'valid', intent: { idempotencyKey: '55555555-5555-4555-8555-555555555555' } })
  })

  it('removes only the matching intent and requires an explicit forget for invalid data', () => {
    const intent = createCommandIntent(deviceID, key)!
    expect(writeCommandIntent(intent)).toBe(true)
    expect(removeCommandIntent({ ...intent, idempotencyKey: '44444444-4444-4444-8444-444444444444' })).toBe(false)
    expect(removeCommandIntent(intent)).toBe(true)

    sessionStorage.setItem(`pulsegrid.command-intent.v1:${deviceID}`, '{')
    expect(readCommandIntent(deviceID)).toEqual({ kind: 'invalid' })
    expect(forgetUnusableCommandIntent(deviceID)).toBe(true)
    expect(readCommandIntent(deviceID)).toEqual({ kind: 'empty' })
  })

  it('fails closed when browser session storage cannot be read', () => {
    vi.spyOn(window, 'sessionStorage', 'get').mockImplementation(() => {
      throw new DOMException('Access denied', 'SecurityError')
    })
    expect(readCommandIntent(deviceID)).toEqual({ kind: 'unavailable' })
    expect(writeCommandIntent(createCommandIntent(deviceID, key)!)).toBe(false)
  })
})
