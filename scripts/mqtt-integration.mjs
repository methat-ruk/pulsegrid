import { randomBytes, randomUUID } from 'node:crypto'
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import net from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const rootDirectory = process.cwd()
const brokerPort = 11883
const databasePort = 15432
const apiPort = 18080
const projectName = `pulsegrid-mqtt-test-${process.pid}-${Date.now()}`
const password = `test-${randomBytes(18).toString('base64url')}`
const databaseUrl = `postgres://pulsegrid:${password}@127.0.0.1:${databasePort}/pulsegrid_test?sslmode=disable`
const environment = {
  ...process.env,
  PULSEGRID_ENV: 'test',
  PULSEGRID_IDENTITY_MODE: 'development',
  PULSEGRID_MQTT_INGESTION_MODE: 'development',
  PULSEGRID_MQTT_BROKER_URL: `mqtt://127.0.0.1:${brokerPort}`,
  PULSEGRID_DATABASE_URL: databaseUrl,
  PULSEGRID_HTTP_HOST: '127.0.0.1',
  PULSEGRID_HTTP_PORT: String(apiPort),
  PULSEGRID_POSTGRES_PASSWORD: password,
}
const runningChildren = new Set()
let stopRequested = false
let stopCode = 0
let cleanupStarted = false
let apiChild = null
let apiExit = null
let apiOutput = ''

const simulatorDirectory = mkdtempSync(join(tmpdir(), 'pulsegrid-mqtt-'))
const apiBinary = join(simulatorDirectory, 'api')
const simulatorBinary = join(simulatorDirectory, 'device-simulator')

for (const port of [brokerPort, databasePort, apiPort]) {
  if (await isPortOpen(port)) {
    console.error(`refusing MQTT ingestion integration: 127.0.0.1:${port} is already in use`)
    process.exit(1)
  }
}

process.once('SIGINT', () => requestStop('SIGINT', 130))
process.once('SIGTERM', () => requestStop('SIGTERM', 143))

let exitCode = 1
try {
  const built = await buildBinaries()
  const started = built && await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'postgres-test', 'mqtt-test'], environment, 180_000)
  const migrated = started && await prepareDatabase()
  if (migrated && !stopRequested) {
    await startApi()
    await waitForReady()
    const deviceID = await createDevice()
    await publishSimulator(deviceID)
    assertNoRawTelemetryInLogs()
    await publishInvalidCases(deviceID)
    await testRetainedMessage(deviceID)
    await testBrokerRecovery(deviceID)
    if (!await stopApi()) throw new Error('API did not drain and stop cleanly')
    if (!apiOutput.includes('reason_code=shutdown_complete')) throw new Error('API shutdown completion was not logged')
    exitCode = stopRequested ? stopCode : 0
  }
} catch (error) {
  console.error(`MQTT ingestion integration failed: ${error.message}`)
  exitCode = stopRequested ? stopCode : 1
} finally {
  if (apiChild) await stopApi()
  const cleanupSucceeded = await cleanup()
  if (!cleanupSucceeded && !stopRequested) exitCode = 1
  if (stopRequested) exitCode = stopCode
}
process.exit(exitCode)

async function buildBinaries() {
  const apiBuilt = await run('go', ['-C', 'apps/api', 'build', '-o', apiBinary, './cmd/api'], process.env, 180_000)
  const simulatorBuilt = apiBuilt && await run('go', ['-C', 'apps/api', 'build', '-o', simulatorBinary, './cmd/device-simulator'], process.env, 180_000)
  return apiBuilt && simulatorBuilt
}

async function prepareDatabase() {
  return await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'down'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'seed'], environment, 120_000)
}

async function startApi() {
  if (apiChild) throw new Error('API is already running')
  apiOutput = ''
  apiChild = spawn(apiBinary, [], {
    cwd: rootDirectory,
    env: environment,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  runningChildren.add(apiChild)
  apiChild.stdout.on('data', (chunk) => { apiOutput += chunk.toString() })
  apiChild.stderr.on('data', (chunk) => { apiOutput += chunk.toString() })
  const child = apiChild
  apiExit = new Promise((resolve) => {
    child.once('close', (status, signal) => {
      runningChildren.delete(child)
      resolve({ status: status ?? 1, signal })
    })
    child.once('error', (error) => {
      runningChildren.delete(child)
      resolve({ status: 1, signal: null, error })
    })
  })
}

async function stopApi() {
  if (!apiChild) return true
  const child = apiChild
  const exit = apiExit
  apiChild = null
  child.kill('SIGTERM')
  const result = await Promise.race([exit, delay(15_000).then(() => ({ status: 1, signal: 'timeout' }))])
  if (result.status !== 0) {
    child.kill('SIGKILL')
    console.error(`API process did not exit cleanly: ${JSON.stringify(result)}`)
    return false
  }
  return true
}

async function waitForReady() {
  await waitForHTTP((response) => response.status === 200 && response.body?.status === 'ready', 30_000, 'API readiness')
}

async function waitForNotReady() {
  await waitForHTTP((response) => response.status === 503 && response.body?.reason === 'dependency_unavailable', 15_000, 'API dependency-unavailable readiness')
}

async function waitForHTTP(predicate, timeout, label) {
  const deadline = Date.now() + timeout
  let lastStatus = 'unreachable'
  while (Date.now() < deadline && !stopRequested) {
    try {
      const response = await fetch(`http://127.0.0.1:${apiPort}/health/ready`)
      const body = await response.json()
      lastStatus = `${response.status} ${JSON.stringify(body)}`
      if (predicate({ status: response.status, body })) return
    } catch {
      lastStatus = 'unreachable'
    }
    await delay(150)
  }
  throw new Error(`${label} was not observed before timeout; last state: ${lastStatus}`)
}

async function createDevice() {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'mutation CreateDevice($input: CreateDeviceInput!) { createDevice(input: $input) { id } }',
      variables: { input: { deviceKey: `mqtt-ingestion-${randomUUID()}`, displayName: 'MQTT ingestion integration' } },
    }),
  })
  const body = await response.json()
  const deviceID = body?.data?.createDevice?.id
  if (response.status !== 200 || typeof deviceID !== 'string') {
    throw new Error(`device registration failed: ${JSON.stringify(body)}`)
  }
  return deviceID
}

async function publishSimulator(deviceID) {
  const result = await runCapture(simulatorBinary, [], {
    ...environment,
    PULSEGRID_MQTT_DEVICE_ID: deviceID,
    PULSEGRID_MQTT_TENANT_SLUG: 'pulsegrid-dev',
    PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS: '23.5',
  }, 30_000)
  if (result.status !== 0) throw new Error(`simulator publish failed: ${result.stderr}`)
  const messageMatch = result.stdout.match(/message_id=([0-9a-f-]{36})/u)
  if (!messageMatch) throw new Error(`simulator output did not contain message ID: ${result.stdout}`)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${messageMatch[1]}`], 10_000, 'valid telemetry acceptance')
}

async function publishInvalidCases(deviceID) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const validBoundaryPayload = () => JSON.stringify({ schemaVersion: 1, messageId: randomUUID(), observedAt: pastObservedAt(), temperatureCelsius: 24 })
  const cases = [
    { label: 'malformed payload', payload: '{', reason: 'telemetry_payload_malformed' },
    { label: 'application oversized payload', payload: 'x'.repeat(1100), reason: 'telemetry_payload_too_large' },
    { label: 'wrong tenant', topic: topic.replace('/tenants/pulsegrid-dev/', '/tenants/other-tenant/'), payload: validBoundaryPayload(), reason: 'telemetry_device_not_registered_for_tenant' },
    { label: 'unknown device', topic: topic.replace(deviceID, randomUUID()), payload: validBoundaryPayload(), reason: 'telemetry_device_not_registered_for_tenant' },
  ]
  for (const testCase of cases) {
    const result = await publishRaw(testCase.topic ?? topic, testCase.payload, false)
    if (result.status !== 0) throw new Error(`${testCase.label} publish failed: ${result.stderr}`)
    await waitForLog(`reason_code=${testCase.reason}`, 10_000, testCase.label)
  }

  const duplicateMessageID = '22222222-2222-4222-8222-222222222222'
  const duplicatePayload = JSON.stringify({ schemaVersion: 1, messageId: duplicateMessageID, observedAt: pastObservedAt(), temperatureCelsius: 24.5 })
  await publishRaw(topic, duplicatePayload, false)
  await publishRaw(topic, duplicatePayload, false)
  await waitForLogCountFields(['reason_code=telemetry_accepted', `message_id=${duplicateMessageID}`], 2, 10_000, 'duplicate logical message acceptance')
}

async function testRetainedMessage(deviceID) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  if (!await stopApi()) throw new Error('API did not stop before retained-message test')
  const retainedPayload = JSON.stringify({ schemaVersion: 1, messageId: '33333333-3333-4333-8333-333333333333', observedAt: pastObservedAt(), temperatureCelsius: 25 })
  if ((await publishRaw(topic, retainedPayload, true)).status !== 0) throw new Error('retained publish failed')
  await startApi()
  await waitForReady()
  await waitForLog('reason_code=telemetry_retained_rejected', 10_000, 'retained message rejection')
  await publishRaw(topic, '', true)
}

async function testBrokerRecovery(deviceID) {
  const stopped = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'stop', 'mqtt-test'], environment, 60_000)
  if (!stopped) throw new Error('unable to stop test broker for recovery test')
  await waitForNotReady()
  const started = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'mqtt-test'], environment, 120_000)
  if (!started) throw new Error('unable to restart test broker for recovery test')
  await waitForReady()
  const messageID = randomUUID()
  await publishRaw(`pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`, JSON.stringify({ schemaVersion: 1, messageId: messageID, observedAt: pastObservedAt(), temperatureCelsius: 26 }), false)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${messageID}`], 10_000, 'post-recovery telemetry acceptance')
}

function assertNoRawTelemetryInLogs() {
  if (apiOutput.includes('23.5') || apiOutput.includes('schemaVersion') || apiOutput.includes('temperatureCelsius')) {
    throw new Error('API logs contain raw telemetry content')
  }
}

async function publishRaw(topic, payload, retained) {
  const args = [
    'docker', 'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', 'mqtt-test',
    'mosquitto_pub', '-h', '127.0.0.1', '-p', '1883', '-i', `pulsegrid-pub-${randomUUID()}`,
    '-q', '1', '-t', topic, '-m', payload,
  ]
  if (retained) args.push('-r')
  return runCapture(args[0], args.slice(1), environment, 15_000)
}

async function waitForLog(text, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (apiOutput.includes(text)) return
    await delay(100)
  }
  throw new Error(`${label} was not found in API output: ${text}\nAPI output:\n${apiOutput}`)
}

async function waitForLogFields(fields, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (fields.every((field) => apiOutput.includes(field))) return
    await delay(100)
  }
  throw new Error(`${label} was not found in API output: ${fields.join(', ')}\nAPI output:\n${apiOutput}`)
}

async function waitForLogCountFields(fields, expected, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (countLogFields(fields) >= expected) return
    await delay(100)
  }
  throw new Error(`${label} did not reach ${expected} occurrences: ${fields.join(', ')}\nAPI output:\n${apiOutput}`)
}

function countLogFields(fields) {
  return apiOutput.split('\n').filter((line) => fields.every((field) => line.includes(field))).length
}

async function cleanup() {
  if (cleanupStarted) return true
  cleanupStarted = true
  let success = true
  if (!await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'], environment, 120_000)) success = false
  try {
    rmSync(simulatorDirectory, { recursive: true, force: true })
  } catch (error) {
    console.error(`unable to remove temporary binaries: ${error.message}`)
    success = false
  }
  return success
}

function run(command, args, env, timeout) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { cwd: rootDirectory, env, stdio: 'inherit' })
    runningChildren.add(child)
    let settled = false
    let timedOut = false
    const timeoutID = setTimeout(() => {
      timedOut = true
      child.kill('SIGTERM')
      setTimeout(() => child.kill('SIGKILL'), 5_000).unref()
    }, timeout)
    const finish = (success) => {
      if (settled) return
      settled = true
      clearTimeout(timeoutID)
      runningChildren.delete(child)
      resolve(success && !timedOut)
    }
    child.once('error', (error) => {
      console.error(`unable to run ${command}: ${error.message}`)
      finish(false)
    })
    child.once('close', (status) => finish(status === 0))
  })
}

function runCapture(command, args, env, timeout) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { cwd: rootDirectory, env, stdio: ['ignore', 'pipe', 'pipe'] })
    runningChildren.add(child)
    let stdout = ''
    let stderr = ''
    let timedOut = false
    let settled = false
    const timeoutID = setTimeout(() => {
      timedOut = true
      child.kill('SIGTERM')
      setTimeout(() => child.kill('SIGKILL'), 5_000).unref()
    }, timeout)
    child.stdout.on('data', (chunk) => { stdout += chunk.toString() })
    child.stderr.on('data', (chunk) => { stderr += chunk.toString() })
    const finish = (status, signal, errorMessage = '') => {
      if (settled) return
      settled = true
      clearTimeout(timeoutID)
      runningChildren.delete(child)
      resolve({ status: timedOut ? 1 : (status ?? 1), signal, stdout, stderr: `${stderr}${errorMessage}` })
    }
    child.once('close', finish)
    child.once('error', (error) => finish(1, null, error.message))
  })
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

function pastObservedAt() {
  return new Date(Date.now() - 60_000).toISOString()
}

function isPortOpen(port) {
  return new Promise((resolve) => {
    const socket = net.createConnection({ host: '127.0.0.1', port })
    const finish = (open) => {
      socket.destroy()
      resolve(open)
    }
    socket.once('connect', () => finish(true))
    socket.once('error', () => finish(false))
    socket.setTimeout(500, () => finish(false))
  })
}

function requestStop(signal, code) {
  if (stopRequested) return
  stopRequested = true
  stopCode = code
  for (const child of runningChildren) child.kill(signal)
}
