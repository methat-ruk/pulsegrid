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
const activeProcessGroups = new Set()
let managedWebServerGroup
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
        && await startNuxtTestServer(environment)
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
  if (managedWebServerGroup) {
    const stopped = await stopProcessGroup(managedWebServerGroup, 'Nuxt test server')
    managedWebServerGroup = undefined
    if (!stopped) exitCode = 1
  }
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
    PULSEGRID_BROWSER_RUNNER_OWNS_WEB_SERVER: 'true',
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
    for (const processGroup of activeProcessGroups) forceProcessGroupTermination(processGroup)
    return
  }
  interruptedSignal = signal
  for (const processGroup of activeProcessGroups) beginProcessGroupTermination(processGroup)
}

async function run(command, args, env, timeoutMilliseconds, cleanup = false) {
  if (interruptedSignal && !cleanup) return false
  const processGroup = spawnOwnedProcess(command, args, env, 'inherit')
  let timedOut = false
  const timeout = setTimeout(() => {
    timedOut = true
    beginProcessGroupTermination(processGroup)
  }, timeoutMilliseconds)
  const result = await processGroup.closed
  clearTimeout(timeout)
  const stopped = await finishProcessGroup(processGroup, command)
  return result.status === 0 && result.signal === null && !timedOut && !processGroup.spawnError && (!interruptedSignal || cleanup) && stopped.processGroupExited && !processGroup.killError
}

async function startNuxtTestServer(env) {
  const processGroup = spawnOwnedProcess('node', ['scripts/start-nuxt-test-server.mjs'], env, 'inherit')
  managedWebServerGroup = processGroup
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline && !interruptedSignal) {
    if (processGroup.closeResult) {
      console.error(`Nuxt test server exited before readiness: ${processGroup.closeResult.status ?? processGroup.closeResult.signal}`)
      await finishProcessGroup(processGroup, 'Nuxt test server')
      managedWebServerGroup = undefined
      return false
    }
    if (await isPortOpen(ports.web)) return true
    await new Promise(resolve => setTimeout(resolve, 100))
  }

  if (!processGroup.closeResult) beginProcessGroupTermination(processGroup)
  const stopped = await finishProcessGroup(processGroup, 'Nuxt test server')
  managedWebServerGroup = undefined
  if (!stopped.processGroupExited || processGroup.killError) return false
  if (interruptedSignal) return false
  console.error('Nuxt test server did not become ready within 30 seconds')
  return false
}

function spawnOwnedProcess(command, args, env, stdio) {
  const child = spawn(command, args, {
    cwd: process.cwd(),
    env,
    detached: true,
    stdio,
  })
  const processGroup = {
    child,
    pid: child.pid,
    terminationRequested: false,
    forceKillRequested: false,
    escalationTimer: undefined,
    killError: undefined,
    closeResult: undefined,
    spawnError: undefined,
  }
  activeProcessGroups.add(processGroup)
  processGroup.closed = new Promise((resolve) => {
    child.once('error', (error) => {
      processGroup.spawnError = error
      console.error(`unable to run ${command}: ${error.message}`)
    })
    child.once('spawn', () => {
      processGroup.pid = child.pid
      if (processGroup.forceKillRequested) forceProcessGroupTermination(processGroup)
      else if (processGroup.terminationRequested) beginProcessGroupTermination(processGroup)
    })
    child.once('close', (status, signal) => {
      processGroup.closeResult = { status, signal }
      resolve(processGroup.closeResult)
    })
  })
  return processGroup
}

async function stopProcessGroup(processGroup, label) {
  if (!processGroup.closeResult) beginProcessGroupTermination(processGroup)
  const stopped = await finishProcessGroup(processGroup, label)
  return Boolean(stopped.closeResult && stopped.processGroupExited && !processGroup.killError)
}

async function finishProcessGroup(processGroup, label) {
  let closeResult = processGroup.closeResult
  if (!closeResult) closeResult = await waitForProcessClose(processGroup, 5_000)
  if (!closeResult) {
    console.error(`${label} leader did not exit after SIGTERM; sending SIGKILL`)
    forceProcessGroupTermination(processGroup)
    closeResult = await waitForProcessClose(processGroup, 2_000)
  }

  const unexpectedDescendants = Boolean(closeResult && isProcessGroupRunning(processGroup.pid) && !processGroup.terminationRequested)
  if (closeResult && isProcessGroupRunning(processGroup.pid)) {
    if (unexpectedDescendants) console.error(`${label} exited while an owned descendant process remained; cleaning up its process group`)
    beginProcessGroupTermination(processGroup)
  }
  let processGroupExited = await waitForProcessGroupExit(processGroup, 5_000)
  if (!processGroupExited) {
    console.error(`${label} process group did not exit after SIGTERM; sending SIGKILL`)
    forceProcessGroupTermination(processGroup)
    processGroupExited = await waitForProcessGroupExit(processGroup, 2_000)
  }
  if (processGroup.escalationTimer) clearTimeout(processGroup.escalationTimer)
  if (!processGroupExited) console.error(`${label} process group is still alive after bounded termination`)
  if (processGroup.killError) console.error(`unable to fully signal ${label} process group: ${processGroup.killError.message}`)
  activeProcessGroups.delete(processGroup)
  return { closeResult, processGroupExited, unexpectedDescendants }
}

async function waitForProcessClose(processGroup, timeoutMilliseconds) {
  if (processGroup.closeResult) return processGroup.closeResult
  let timer
  const timeout = new Promise(resolve => {
    timer = setTimeout(() => resolve(undefined), timeoutMilliseconds)
  })
  try {
    return await Promise.race([processGroup.closed, timeout])
  }
  finally {
    clearTimeout(timer)
  }
}

function beginProcessGroupTermination(processGroup) {
  processGroup.terminationRequested = true
  if (!processGroup.pid) return
  signalProcessGroup(processGroup, 'SIGTERM')
  if (!processGroup.escalationTimer) {
    processGroup.escalationTimer = setTimeout(() => signalProcessGroup(processGroup, 'SIGKILL'), 2_000)
    processGroup.escalationTimer.unref?.()
  }
}

function forceProcessGroupTermination(processGroup) {
  processGroup.forceKillRequested = true
  if (processGroup.escalationTimer) {
    clearTimeout(processGroup.escalationTimer)
    processGroup.escalationTimer = undefined
  }
  signalProcessGroup(processGroup, 'SIGKILL')
}

function signalProcessGroup(processGroup, signal) {
  if (!processGroup.pid) return
  try {
    process.kill(-processGroup.pid, signal)
  }
  catch (error) {
    if (error.code === 'ESRCH') return
    processGroup.killError = error
    try {
      processGroup.child.kill(signal)
    }
    catch (fallbackError) {
      processGroup.killError = fallbackError
    }
  }
}

function isProcessGroupRunning(processGroupID) {
  if (!processGroupID) return false
  try {
    process.kill(-processGroupID, 0)
    return true
  }
  catch (error) {
    return error.code !== 'ESRCH'
  }
}

async function waitForProcessGroupExit(processGroup, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    if (!isProcessGroupRunning(processGroup.pid)) return true
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  return !isProcessGroupRunning(processGroup.pid)
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
