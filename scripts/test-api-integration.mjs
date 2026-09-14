import { randomBytes } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import net from 'node:net'

const testPort = 15432
const projectName = `pulsegrid-test-${process.pid}-${Date.now()}`
const password = `test-${randomBytes(18).toString('base64url')}`
const databaseUrl = `postgres://pulsegrid:${encodeURIComponent(password)}@127.0.0.1:${testPort}/pulsegrid_test?sslmode=disable`
const environment = {
  ...process.env,
  PULSEGRID_ENV: 'test',
  PULSEGRID_DATABASE_URL: databaseUrl,
  PULSEGRID_POSTGRES_PASSWORD: password,
}

if (await isPortOpen(testPort)) {
  console.error(`refusing to run integration tests: 127.0.0.1:${testPort} is already in use`)
  process.exit(1)
}

let exitCode = 1
try {
  if (!run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'postgres-test'], environment, 180_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000)) {
    exitCode = 1
  } else if (!run('go', ['-C', 'apps/api', 'test', '-count=1', '-tags', 'integration', './...'], environment, 180_000)) {
    exitCode = 1
  } else {
    exitCode = 0
  }
} finally {
  const cleanupSucceeded = run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'], environment, 120_000)
  if (!cleanupSucceeded) exitCode = 1
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
