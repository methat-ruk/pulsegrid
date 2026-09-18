import { randomBytes, randomUUID } from 'node:crypto'
import { spawn, spawnSync } from 'node:child_process'
import net from 'node:net'

const rootDirectory = process.cwd()
const testPort = 11883
const projectName = `pulsegrid-mqtt-test-${process.pid}-${Date.now()}`
const password = `test-${randomBytes(18).toString('base64url')}`
const composeEnvironment = {
  ...process.env,
  PULSEGRID_POSTGRES_PASSWORD: password,
}

if (await isPortOpen(testPort)) {
  console.error(`refusing to run MQTT integration: 127.0.0.1:${testPort} is already in use`)
  process.exit(1)
}

let cleanupStarted = false
const cleanup = () => {
  if (cleanupStarted) return true
  cleanupStarted = true
  return run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '--remove-orphans'], composeEnvironment, 120_000)
}

process.once('SIGINT', () => {
  cleanup()
  process.exit(130)
})
process.once('SIGTERM', () => {
  cleanup()
  process.exit(143)
})

let exitCode = 1
try {
  const started = run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'mqtt-test'], composeEnvironment, 180_000)
  if (!started) {
    exitCode = 1
  } else if (!(await exerciseBroker('initial publish'))) {
    exitCode = 1
  } else if (!run('docker', ['compose', '-p', projectName, '--profile', 'test', 'restart', 'mqtt-test'], composeEnvironment, 60_000)) {
    exitCode = 1
  } else if (!run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'mqtt-test'], composeEnvironment, 180_000)) {
    exitCode = 1
  } else if (!(await exerciseBroker('publish after broker restart'))) {
    exitCode = 1
  } else {
    exitCode = 0
  }
} finally {
  if (!cleanup()) exitCode = 1
}
process.exit(exitCode)

async function exerciseBroker(label) {
  const deviceID = randomUUID()
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const subscriber = spawnSubscriber(topic, 10)
  await delay(500)

  const publisherEnvironment = {
    ...process.env,
    PULSEGRID_ENV: 'test',
    PULSEGRID_MQTT_BROKER_URL: `mqtt://127.0.0.1:${testPort}`,
    PULSEGRID_MQTT_TENANT_SLUG: 'pulsegrid-dev',
    PULSEGRID_MQTT_DEVICE_ID: deviceID,
    PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS: '23.5',
  }
  const published = run('go', ['-C', 'apps/api', 'run', './cmd/device-simulator'], publisherEnvironment, 30_000)
  const received = await subscriber.result
  if (!published) {
    console.error(`${label}: simulator publish failed`)
    return false
  }
  if (received.status !== 0 || received.stdout.trim() === '') {
    console.error(`${label}: subscriber did not receive one message`, received.stderr.trim())
    return false
  }

  const firstLine = received.stdout.trim().split(/\r?\n/u)[0]
  const separator = firstLine.indexOf(' ')
  const receivedTopic = separator === -1 ? '' : firstLine.slice(0, separator)
  const rawPayload = separator === -1 ? '' : firstLine.slice(separator + 1)
  if (receivedTopic !== topic) {
    console.error(`${label}: received topic does not match the simulator topic`)
    return false
  }
  let payload
  try {
    payload = JSON.parse(rawPayload)
  } catch {
    console.error(`${label}: received payload is not JSON`)
    return false
  }
  const payloadKeys = Object.keys(payload).sort().join(',')
  if (payloadKeys !== 'messageId,observedAt,schemaVersion,temperatureCelsius' || payload.schemaVersion !== 1 || payload.temperatureCelsius !== 23.5 || typeof payload.messageId !== 'string' || typeof payload.observedAt !== 'string' || !payload.observedAt.endsWith('Z')) {
    console.error(`${label}: received payload does not match telemetry v1`)
    return false
  }

  const lateSubscriber = spawnSubscriber(topic, 2)
  const lateResult = await lateSubscriber.result
  if (lateResult.status === 0 || lateResult.stdout.trim() !== '') {
    console.error(`${label}: message was retained unexpectedly`)
    return false
  }
  const oversizedSubscriber = spawnSubscriber(topic, 2)
  await delay(300)
  const oversized = runCapture('docker', [
    'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', 'mqtt-test',
    'mosquitto_pub', '-h', '127.0.0.1', '-p', '1883', '-i', 'pulsegrid-oversized',
    '-q', '1', '-t', topic, '-m', 'x'.repeat(17000),
  ], composeEnvironment, 10_000)
  const oversizedReceived = await oversizedSubscriber.result
  if (oversizedReceived.stdout.trim() !== '') {
    console.error(`${label}: broker forwarded an oversized publish`)
    return false
  }
  console.log(`${label}: QoS 1 payload, retain=false, and 16 KiB broker cap verified (MQTT 3.1.1 publish status ${oversized.status})`)
  return true
}

function spawnSubscriber(topic, timeoutSeconds) {
  const child = spawn('docker', [
    'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', 'mqtt-test',
    'mosquitto_sub', '-h', '127.0.0.1', '-p', '1883', '-t', topic, '-q', '1', '-v', '-C', '1', '-W', String(timeoutSeconds),
  ], {
    cwd: rootDirectory,
    env: composeEnvironment,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let stdout = ''
  let stderr = ''
  child.stdout.on('data', (chunk) => { stdout += chunk })
  child.stderr.on('data', (chunk) => { stderr += chunk })

  const result = new Promise((resolve) => {
    child.once('close', (status, signal) => resolve({ status: status ?? 1, signal, stdout, stderr }))
    child.once('error', (error) => resolve({ status: 1, signal: null, stdout, stderr: `${stderr}${error.message}` }))
  })
  return { child, result }
}

function run(command, args, env, timeout) {
  const result = spawnSync(command, args, {
    cwd: rootDirectory,
    env,
    stdio: 'inherit',
    timeout,
  })
  if (result.error) {
    console.error(`unable to run ${command}: ${result.error.message}`)
    return false
  }
  return result.status === 0
}

function runCapture(command, args, env, timeout) {
  const result = spawnSync(command, args, {
    cwd: rootDirectory,
    env,
    encoding: 'utf8',
    timeout,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  return {
    status: result.status ?? 1,
    stdout: result.stdout ?? '',
    stderr: result.stderr ?? '',
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
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
