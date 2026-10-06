import { randomBytes } from 'node:crypto'
import { spawn } from 'node:child_process'
import net from 'node:net'

const ports = { database: 15432, broker: 11883, api: 18080, web: 4173 }
const projectName = `pulsegrid-browser-${process.pid}-${Date.now()}`
const playwrightArguments = process.argv.slice(2).filter(argument => argument !== '--')
const externalDatabaseMode = process.env.CI === 'true'
  && process.env.PULSEGRID_BROWSER_TEST_DATABASE_MODE === 'external-ci-service'
const ownsDatabase = !externalDatabaseMode
const password = `browser-${randomBytes(18).toString('base64url')}`
const databaseUrl = externalDatabaseMode
  ? process.env.PULSEGRID_DATABASE_URL ?? ''
  : process.env.PULSEGRID_DATABASE_URL ?? `postgres://pulsegrid:${encodeURIComponent(password)}@127.0.0.1:${ports.database}/pulsegrid_test?sslmode=disable`
const environment = testEnvironment(databaseUrl, password)
let resourcesMayExist = false
let interruptedSignal
let activeChild
let exitCode = 1

process.on('SIGINT', () => requestStop('SIGINT'))
process.on('SIGTERM', () => requestStop('SIGTERM'))

try {
  if (process.env.PULSEGRID_BROWSER_TEST_DATABASE_MODE && process.env.CI !== 'true') {
    console.error('refusing browser smoke tests: external database mode is CI-only')
  }
  else if (!externalDatabaseMode && process.env.PULSEGRID_DATABASE_URL) {
    console.error('refusing browser smoke tests: external database URLs require the explicit CI-owned database mode')
  }
  else if (externalDatabaseMode && !isIsolatedTestDatabaseURL(databaseUrl)) {
    console.error('refusing browser smoke tests: external database target must be the isolated loopback test database')
  }
  else {
    const checkedPorts = Object.entries(ports).filter(([name]) => ownsDatabase || name !== 'database')
    const occupied = []
    for (const [name, port] of checkedPorts) {
      if (await isPortOpen(port)) occupied.push(`${name}=127.0.0.1:${port}`)
    }

    if (occupied.length > 0) {
      console.error(`refusing browser smoke tests: owned test port(s) already in use: ${occupied.join(', ')}`)
    }
    else {
      resourcesMayExist = true
      if (
        await run('docker', [
          'compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait',
          ...(ownsDatabase ? ['postgres-test', 'mqtt-test'] : ['mqtt-test']),
        ], environment, 180_000)
        && await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)
        && await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'seed'], environment, 120_000)
        && await run('node', ['scripts/build-api-test.mjs'], environment, 180_000)
        && await run('node', ['scripts/build-web.mjs', 'test'], environment, 180_000)
        && await run('pnpm', ['exec', 'playwright', 'test', ...playwrightArguments], environment, 360_000)
      ) {
        exitCode = 0
      }
    }
  }
}
catch (error) {
  console.error(`browser smoke setup failed: ${error.message}`)
  exitCode = 1
}
finally {
  if (resourcesMayExist) {
    const cleanupSucceeded = await run(
      'docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'],
      environment,
      120_000,
      true,
    )
    if (!cleanupSucceeded) exitCode = 1
  }
  if (interruptedSignal) exitCode = interruptedSignal === 'SIGINT' ? 130 : 143
}

process.exitCode = exitCode

function isIsolatedTestDatabaseURL(value) {
  try {
    const url = new URL(value)
    const queryEntries = [...url.searchParams.entries()]
    return ['postgres:', 'postgresql:'].includes(url.protocol)
      && url.hostname === '127.0.0.1'
      && url.port === String(ports.database)
      && url.pathname === '/pulsegrid_test'
      && decodeURIComponent(url.username) === 'pulsegrid'
      && decodeURIComponent(url.password).trim() !== ''
      && decodeURIComponent(url.password) !== 'CHANGE_ME'
      && url.hash === ''
      && queryEntries.length === 1
      && queryEntries[0][0] === 'sslmode'
      && queryEntries[0][1] === 'disable'
  }
  catch {
    return false
  }
}

function testEnvironment(testDatabaseUrl, testPassword) {
  const clean = { ...process.env }
  for (const key of Object.keys(clean)) {
    if (key.startsWith('PULSEGRID_') || key.startsWith('NUXT_')) delete clean[key]
  }
  return {
    ...clean,
    PULSEGRID_ENV: 'test',
    PULSEGRID_IDENTITY_MODE: 'development',
    PULSEGRID_MQTT_INGESTION_MODE: 'development',
    PULSEGRID_MQTT_COMMAND_MODE: 'development',
    PULSEGRID_DATABASE_URL: testDatabaseUrl,
    PULSEGRID_POSTGRES_PASSWORD: testPassword,
    PULSEGRID_HTTP_HOST: '127.0.0.1',
    PULSEGRID_HTTP_PORT: String(ports.api),
    PULSEGRID_MQTT_BROKER_URL: `mqtt://127.0.0.1:${ports.broker}`,
    PULSEGRID_LOG_LEVEL: 'error',
    PULSEGRID_SHUTDOWN_TIMEOUT: '1s',
    NUXT_APP_ENV: 'test',
    NUXT_BACKEND_ORIGIN: `http://127.0.0.1:${ports.api}`,
    NUXT_TELEMETRY_DISABLED: '1',
  }
}

function requestStop(signal) {
  if (interruptedSignal) {
    if (activeChild) signalProcessGroup(activeChild, 'SIGKILL')
    return
  }
  interruptedSignal = signal
  if (activeChild) signalProcessGroup(activeChild, signal)
}

async function run(command, args, env, timeoutMilliseconds, cleanup = false) {
  if (interruptedSignal && !cleanup) return false
  return await new Promise((resolve) => {
    let timedOut = false
    let escalationTimer
    const child = spawn(command, args, {
      cwd: process.cwd(),
      env,
      detached: true,
      stdio: 'inherit',
    })
    activeChild = child
    const timeout = setTimeout(() => {
      timedOut = true
      signalProcessGroup(child, 'SIGTERM')
      escalationTimer = setTimeout(() => signalProcessGroup(child, 'SIGKILL'), 2_000)
    }, timeoutMilliseconds)
    child.once('error', (error) => {
      clearTimeout(timeout)
      if (escalationTimer) clearTimeout(escalationTimer)
      if (activeChild === child) activeChild = undefined
      console.error(`unable to run ${command}: ${error.message}`)
      resolve(false)
    })
    child.once('close', (status, signal) => {
      clearTimeout(timeout)
      if (escalationTimer) clearTimeout(escalationTimer)
      if (activeChild === child) activeChild = undefined
      resolve(status === 0 && signal === null && !timedOut)
    })
  })
}

function signalProcessGroup(child, signal) {
  if (!child.pid) return
  try {
    process.kill(-child.pid, signal)
  }
  catch {
    child.kill(signal)
  }
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
