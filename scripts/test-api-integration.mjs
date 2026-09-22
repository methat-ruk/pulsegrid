import { randomBytes } from 'node:crypto'
import { spawn, spawnSync } from 'node:child_process'
import net from 'node:net'

const testPort = 15432
const startupPort = 18081
const projectName = `pulsegrid-test-${process.pid}-${Date.now()}`
const password = `test-${randomBytes(18).toString('base64url')}`
const databaseUrl = `postgres://pulsegrid:${encodeURIComponent(password)}@127.0.0.1:${testPort}/pulsegrid_test?sslmode=disable`
const environment = {
  ...process.env,
  PULSEGRID_ENV: 'test',
  PULSEGRID_IDENTITY_MODE: 'development',
  PULSEGRID_DATABASE_URL: databaseUrl,
  PULSEGRID_POSTGRES_PASSWORD: password,
}

for (const port of [testPort, startupPort]) {
  if (await isPortOpen(port)) {
    console.error(`refusing to run integration tests: 127.0.0.1:${port} is already in use`)
    process.exit(1)
  }
}

let exitCode = 1
try {
  if (!run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'postgres-test'], environment, 180_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'down'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'test', '-count=1', '-tags', 'integration', './cmd/api', '-run', '^TestOpenDevelopmentGraphQLRejectsMissingTelemetrySchema$'], environment, 180_000)) {
    exitCode = 1
  } else if (!await runApiAgainstMissingSchema()) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'test', '-race', '-count=1', '-tags', 'integration', './...'], environment, 180_000)) {
    exitCode = 1
  } else {
    exitCode = 0
  }
} finally {
  const cleanupSucceeded = run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'], environment, 120_000)
  if (!cleanupSucceeded) exitCode = 1
}
process.exit(exitCode)

async function runApiAgainstMissingSchema() {
  const childEnvironment = {
    ...environment,
    PULSEGRID_HTTP_HOST: '127.0.0.1',
    PULSEGRID_HTTP_PORT: String(startupPort),
    PULSEGRID_MQTT_INGESTION_MODE: 'disabled',
  }
  return await new Promise((resolve) => {
    const child = spawn('go', ['-C', 'apps/api', 'run', './cmd/api'], {
      cwd: process.cwd(),
      env: childEnvironment,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let output = ''
    let settled = false
    const timeout = setTimeout(() => {
      if (settled) return
      child.kill('SIGTERM')
      setTimeout(() => child.kill('SIGKILL'), 5_000).unref()
    }, 30_000)
    child.stdout.on('data', (chunk) => { output += chunk.toString() })
    child.stderr.on('data', (chunk) => { output += chunk.toString() })
    const finish = async (status) => {
      if (settled) return
      settled = true
      clearTimeout(timeout)
      const portOpen = await isPortOpen(startupPort)
      const passed = status !== 0 && output.includes('reason_code=database_schema_unavailable') && !portOpen
      if (!passed) console.error(`pre-005 API startup output/status invalid: status=${status}\n${output}`)
      resolve(passed)
    }
    child.once('error', () => finish(1))
    child.once('close', (status) => finish(status ?? 1))
  })
}

function run(command, args, env, timeout) {
  const result = spawnSync(command, args, {
    cwd: process.cwd(),
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
