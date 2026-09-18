import { spawnSync } from 'node:child_process'
import net from 'node:net'

const rootDirectory = process.cwd()
const operation = process.argv[2]
const environment = {
  ...process.env,
  // Compose interpolates the PostgreSQL service even when only mqtt-dev is
  // selected. This value is never passed to the MQTT container.
  PULSEGRID_POSTGRES_PASSWORD: process.env.PULSEGRID_POSTGRES_PASSWORD ?? 'compose-unused-placeholder',
}

if (!['up', 'stop', 'down', 'logs', 'health'].includes(operation)) {
  console.error('usage: node scripts/mqtt-dev.mjs <up|stop|down|logs|health>')
  process.exit(2)
}

if (operation === 'up' && await isPortOpen(1883) && !serviceIsRunning()) {
  console.error('refusing to start mqtt-dev: 127.0.0.1:1883 is already in use')
  process.exit(1)
}

const args = operation === 'up'
  ? ['compose', '--profile', 'dev', 'up', '-d', '--wait', 'mqtt-dev']
  : operation === 'stop'
    ? ['compose', '--profile', 'dev', 'stop', 'mqtt-dev']
    : operation === 'down'
      ? ['compose', '--profile', 'dev', 'rm', '-s', '-f', 'mqtt-dev']
      : operation === 'logs'
        ? ['compose', '--profile', 'dev', 'logs', '--no-color', '--tail=200', 'mqtt-dev']
        : ['compose', '--profile', 'dev', 'ps', 'mqtt-dev']

const result = spawnSync('docker', args, {
  cwd: rootDirectory,
  env: environment,
  stdio: 'inherit',
})

if (result.error) {
  console.error(`unable to run docker compose: ${result.error.message}`)
  process.exit(1)
}
process.exit(result.status ?? 1)

function serviceIsRunning() {
  const result = spawnSync('docker', ['compose', '--profile', 'dev', 'ps', '--status', 'running', '--services', 'mqtt-dev'], {
    cwd: rootDirectory,
    env: environment,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  })
  return result.status === 0 && result.stdout.trim() === 'mqtt-dev'
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
