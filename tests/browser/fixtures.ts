import { once } from 'node:events'
import { spawn, type ChildProcess } from 'node:child_process'
import { access } from 'node:fs/promises'
import { createConnection } from 'node:net'
import { join } from 'node:path'

import { test as base } from '@playwright/test'

const apiHost = '127.0.0.1'
const apiPort = 18080
const apiURL = `http://${apiHost}:${apiPort}/health/ready`
const apiBinary = join(process.cwd(), '.output', 'api-test')
const simulatorBinary = join(process.cwd(), '.output', 'device-simulator-test')

export type ApiProcess = {
  start: () => Promise<void>
  stop: () => Promise<void>
}

export async function assertPortAvailable(): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const socket = createConnection({ host: apiHost, port: apiPort })
    const timer = setTimeout(() => finish(new Error(`API test port ${apiPort} availability check timed out`)), 500)

    const finish = (error?: Error) => {
      clearTimeout(timer)
      socket.destroy()
      if (error) reject(error)
      else resolve()
    }

    socket.once('connect', () => finish(new Error(`API test port ${apiPort} is already in use`)))
    socket.once('error', (error: NodeJS.ErrnoException) => {
      if (error.code === 'ECONNREFUSED') finish()
      else finish(error)
    })
  })
}

async function waitForAPI(child: ChildProcess): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (child.exitCode !== null) throw new Error(`API process exited with code ${child.exitCode}`)
    try {
      const response = await fetch(apiURL)
      if (response.status === 200) return
    } catch {
      // The listener may still be starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('API process did not become ready within 5 seconds')
}

async function stopProcess(child: ChildProcess | undefined): Promise<void> {
  if (!child || child.exitCode !== null) return
  const exited = once(child, 'exit')
  child.kill('SIGTERM')
  const graceful = await Promise.race([
    exited.then(() => true),
    new Promise<boolean>(resolve => setTimeout(() => resolve(false), 2_000)),
  ])
  if (!graceful && child.exitCode === null) {
    child.kill('SIGKILL')
    await exited
  }
}

async function createAPIProcess(): Promise<ApiProcess> {
  await access(apiBinary)
  const databaseURL = process.env.PULSEGRID_DATABASE_URL
  if (!databaseURL) throw new Error('PULSEGRID_DATABASE_URL is required for browser smoke tests')
  let child: ChildProcess | undefined

  const start = async () => {
    if (child && child.exitCode === null) return
    await assertPortAvailable()
    child = spawn(apiBinary, [], {
      env: {
        ...process.env,
        PULSEGRID_ENV: 'test',
        PULSEGRID_IDENTITY_MODE: 'development',
        PULSEGRID_MQTT_INGESTION_MODE: 'development',
        PULSEGRID_MQTT_BROKER_URL: 'mqtt://127.0.0.1:11883',
        PULSEGRID_DATABASE_URL: databaseURL,
        PULSEGRID_HTTP_HOST: '127.0.0.1',
        PULSEGRID_HTTP_PORT: '18080',
        PULSEGRID_LOG_LEVEL: 'error',
        PULSEGRID_SHUTDOWN_TIMEOUT: '1s',
      },
      stdio: 'ignore',
    })
    try {
      await waitForAPI(child)
    }
    catch (error) {
      await stopProcess(child)
      child = undefined
      throw error
    }
  }

  const stop = async () => {
    await stopProcess(child)
    child = undefined
  }

  await start()
  return { start, stop }
}

export const test = base.extend<{}, { apiProcess: ApiProcess }>({
  apiProcess: [async ({}, use) => {
    const api = await createAPIProcess()
    try {
      await use(api)
    } finally {
      await api.stop()
    }
  }, { scope: 'worker', auto: true }],
})

export async function publishSimulatorTelemetry(deviceID: string, temperatureCelsius: number): Promise<{ messageID: string }> {
  await access(simulatorBinary)
  const child = spawn(simulatorBinary, [], {
    env: {
      ...process.env,
      PULSEGRID_ENV: 'test',
      PULSEGRID_MQTT_BROKER_URL: 'mqtt://127.0.0.1:11883',
      PULSEGRID_MQTT_TENANT_SLUG: 'pulsegrid-dev',
      PULSEGRID_MQTT_DEVICE_ID: deviceID,
      PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS: String(temperatureCelsius),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let stdout = ''
  let stderr = ''
  child.stdout?.on('data', chunk => { stdout += chunk.toString() })
  child.stderr?.on('data', chunk => { stderr += chunk.toString() })
  const exit = await new Promise<{ status: number | null, signal: NodeJS.Signals | null }>((resolve, reject) => {
    child.once('error', reject)
    child.once('close', (status, signal) => resolve({ status, signal }))
  })
  if (exit.status !== 0) throw new Error(`simulator publish failed (${exit.status ?? exit.signal}): ${stderr}`)
  const messageMatch = stdout.match(/message_id=([0-9a-f-]{36})/u)
  if (!messageMatch) throw new Error(`simulator output did not contain message ID: ${stdout}`)
  return { messageID: messageMatch[1] }
}

export async function seedAlertDevice(page: import('@playwright/test').Page, deviceKey: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: `mutation CreateDevice($input: CreateDeviceInput!) {
        createDevice(input: $input) { id deviceKey displayName }
      }`,
      variables: { input: { deviceKey, displayName: 'Alert browser device' } },
    },
  })
  if (response.status() !== 200) throw new Error(`device creation returned ${response.status()}`)
  const payload = await response.json()
  if (payload.errors !== undefined) throw new Error('device creation returned GraphQL errors')
  return payload.data.createDevice as { id: string, deviceKey: string, displayName: string }
}

export async function createTemperatureRule(page: import('@playwright/test').Page, deviceID: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: `mutation CreateRule($input: CreateThresholdRuleInput!) {
        createThresholdRule(input: $input) { id enabled comparator thresholdCelsius }
      }`,
      variables: { input: { deviceId: deviceID, comparator: 'GT', thresholdCelsius: 25 } },
    },
  })
  if (response.status() !== 200) throw new Error(`rule creation returned ${response.status()}`)
  const payload = await response.json()
  if (payload.errors !== undefined) throw new Error('rule creation returned GraphQL errors')
  return payload.data.createThresholdRule as { id: string, enabled: boolean }
}

export async function readAlerts(page: import('@playwright/test').Page, deviceID?: string) {
  const response = await page.request.post('/api/graphql', {
    headers: { accept: 'application/graphql-response+json' },
    data: {
      query: `query Alerts($first: Int!, $deviceId: ID) {
        alerts(first: $first, deviceId: $deviceId) {
          edges { node { id deviceId ruleId messageId observedAt receivedAt temperatureCelsius metric comparator thresholdCelsius createdAt } }
          pageInfo { endCursor hasNextPage }
        }
      }`,
      variables: { first: 50, deviceId: deviceID ?? null },
    },
  })
  if (response.status() !== 200) throw new Error(`alert query returned ${response.status()}`)
  const payload = await response.json()
  if (payload.errors !== undefined) throw new Error('alert query returned GraphQL errors')
  return payload.data.alerts as {
    edges: Array<{ node: {
      id: string
      deviceId: string
      ruleId: string
      messageId: string
      observedAt: string
      receivedAt: string
      temperatureCelsius: number
      comparator: string
      thresholdCelsius: number
    } }>
    pageInfo: { endCursor: string | null, hasNextPage: boolean }
  }
}

export { expect } from '@playwright/test'
