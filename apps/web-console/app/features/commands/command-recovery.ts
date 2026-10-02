import { isCanonicalUUID } from './command-graphql'

export type CommandIntent = {
  deviceId: string
  idempotencyKey: string
  commandId: string | null
  type: 'PING'
}

export type ReadIntentResult
  = | { kind: 'empty' }
    | { kind: 'valid', intent: CommandIntent }
    | { kind: 'invalid' }
    | { kind: 'unavailable' }

const STORAGE_PREFIX = 'pulsegrid.command-intent.v1:'
const MAX_RECORD_LENGTH = 1024

function storageKey(deviceId: string): string {
  return `${STORAGE_PREFIX}${deviceId}`
}

function parseIntent(value: unknown, expectedDeviceId: string): CommandIntent | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  const record = value as Record<string, unknown>
  const keys = Object.keys(record)
  if (keys.some(key => !['version', 'deviceId', 'type', 'idempotencyKey', 'commandId'].includes(key))
    || record.version !== 1
    || record.deviceId !== expectedDeviceId
    || !isCanonicalUUID(record.deviceId)
    || record.type !== 'PING'
    || !isCanonicalUUID(record.idempotencyKey)
    || !(record.commandId === null || isCanonicalUUID(record.commandId))) {
    return null
  }
  return {
    deviceId: record.deviceId,
    type: 'PING',
    idempotencyKey: record.idempotencyKey,
    commandId: record.commandId,
  }
}

export function createCommandIntent(deviceId: string, idempotencyKey: string): CommandIntent | null {
  if (!isCanonicalUUID(deviceId) || !isCanonicalUUID(idempotencyKey)) return null
  return { deviceId, idempotencyKey, commandId: null, type: 'PING' }
}

export function readCommandIntent(deviceId: string): ReadIntentResult {
  let value: string | null
  try {
    value = window.sessionStorage.getItem(storageKey(deviceId))
  }
  catch {
    return { kind: 'unavailable' }
  }
  if (value === null) return { kind: 'empty' }
  if (value.length > MAX_RECORD_LENGTH) return { kind: 'invalid' }
  try {
    const intent = parseIntent(JSON.parse(value), deviceId)
    return intent === null ? { kind: 'invalid' } : { kind: 'valid', intent }
  }
  catch {
    return { kind: 'invalid' }
  }
}

export function writeCommandIntent(intent: CommandIntent): boolean {
  const validated = parseIntent({ ...intent, version: 1 }, intent.deviceId)
  if (validated === null) return false
  const value = JSON.stringify({ version: 1, ...validated })
  if (value.length > MAX_RECORD_LENGTH) return false
  try {
    const storage = window.sessionStorage
    storage.setItem(storageKey(intent.deviceId), value)
    return storage.getItem(storageKey(intent.deviceId)) === value
  }
  catch {
    return false
  }
}

export function removeCommandIntent(intent: CommandIntent): boolean {
  try {
    const storage = window.sessionStorage
    const key = storageKey(intent.deviceId)
    const raw = storage.getItem(key)
    if (raw === null) return true
    if (raw.length > MAX_RECORD_LENGTH) return false
    const parsed = parseIntent(JSON.parse(raw), intent.deviceId)
    if (parsed === null || parsed.idempotencyKey !== intent.idempotencyKey) return false
    storage.removeItem(key)
    return storage.getItem(key) === null
  }
  catch {
    return false
  }
}

export function forgetUnusableCommandIntent(deviceId: string): boolean {
  if (!isCanonicalUUID(deviceId)) return false
  try {
    const storage = window.sessionStorage
    storage.removeItem(storageKey(deviceId))
    return storage.getItem(storageKey(deviceId)) === null
  }
  catch {
    return false
  }
}
