import { randomBytes } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import net from 'node:net'

const testPort = 15432
const brokerPort = 11883
const projectName = `pulsegrid-browser-${process.pid}-${Date.now()}`
const ownsDatabase = !process.env.PULSEGRID_DATABASE_URL
const ownsBroker = true
const password = `browser-${randomBytes(18).toString('base64url')}`
const databaseUrl = process.env.PULSEGRID_DATABASE_URL
  ?? `postgres://pulsegrid:${encodeURIComponent(password)}@127.0.0.1:${testPort}/pulsegrid_test?sslmode=disable`
const environment = {
  ...process.env,
  PULSEGRID_ENV: 'test',
  PULSEGRID_IDENTITY_MODE: 'development',
  PULSEGRID_MQTT_INGESTION_MODE: 'development',
  PULSEGRID_MQTT_BROKER_URL: `mqtt://127.0.0.1:${brokerPort}`,
  PULSEGRID_DATABASE_URL: databaseUrl,
  PULSEGRID_POSTGRES_PASSWORD: password,
  NUXT_APP_ENV: 'test',
  NUXT_BACKEND_ORIGIN: 'http://127.0.0.1:18080',
  NUXT_TELEMETRY_DISABLED: '1',
}

let exitCode = 1
try {
  if ((ownsDatabase && await isPortOpen(testPort)) || await isPortOpen(brokerPort)) {
    console.error('refusing to run browser smoke tests: an isolated dependency port is already in use')
  }
  else if (
    run('docker', [
      'compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait',
      ...(ownsDatabase ? ['postgres-test', 'mqtt-test'] : ['mqtt-test']),
    ], environment, 180_000)
    && run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)
    && run('go', ['-C', 'apps/api', 'run', './cmd/db', 'seed'], environment, 120_000)
    && run('node', ['scripts/build-api-test.mjs'], environment, 180_000)
    && run('node', ['scripts/build-web.mjs', 'test'], environment, 180_000)
    && run('pnpm', ['exec', 'playwright', 'test'], environment, 180_000)
  ) {
    exitCode = 0
  }
}
finally {
  if (ownsDatabase || ownsBroker) {
    const cleanupSucceeded = run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'], environment, 120_000)
    if (!cleanupSucceeded) exitCode = 1
  }
}

process.exit(exitCode)

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
