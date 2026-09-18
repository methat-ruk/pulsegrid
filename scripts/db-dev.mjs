import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { URL } from 'node:url'

const rootDirectory = process.cwd()
const dotenvPath = 'apps/api/.env.development'

const operation = process.argv[2]
const providedArguments = process.argv.slice(3)
const operationArguments = providedArguments[0] === '--'
  ? providedArguments.slice(1)
  : providedArguments
const supportedOperations = [
  'up',
  'all-up',
  'stop',
  'all-stop',
  'down',
  'all-down',
  'all-logs',
  'all-health',
  'psql',
]
if (!supportedOperations.includes(operation)) {
  console.error(`usage: node scripts/db-dev.mjs <${supportedOperations.join('|')}> [psql args]`)
  process.exit(2)
}

const credentials = ['up', 'all-up', 'psql'].includes(operation)
  ? loadDevelopmentCredentials()
  : { password: process.env.PULSEGRID_POSTGRES_PASSWORD ?? 'compose-unused-placeholder' }
const environment = {
  ...process.env,
  PULSEGRID_POSTGRES_PASSWORD: credentials.password,
}

const compose = (...composeArguments) => ['compose', '--profile', 'dev', ...composeArguments]
const argsByOperation = {
  up: compose('up', '-d', '--wait', 'postgres-dev'),
  // Compose `up` creates missing containers and starts existing ones.
  'all-up': compose('up', '-d', '--wait', 'postgres-dev', 'mqtt-dev'),
  stop: compose('stop', 'postgres-dev'),
  'all-stop': compose('stop', 'postgres-dev', 'mqtt-dev'),
  down: compose('rm', '-s', '-f', 'postgres-dev'),
  'all-down': compose('down', 'postgres-dev', 'mqtt-dev'),
  'all-logs': compose('logs', '--no-color', '--tail=200', 'postgres-dev', 'mqtt-dev'),
  'all-health': compose('ps', '--format', 'json', 'postgres-dev', 'mqtt-dev'),
  psql: compose(
    'exec',
    ...(process.stdin.isTTY ? [] : ['-T']),
    'postgres-dev',
    'psql',
    '-U',
    'pulsegrid',
    '-d',
    'pulsegrid_dev',
    ...operationArguments,
  ),
}
const args = argsByOperation[operation]

if (operation === 'all-health') {
  const result = spawnSync('docker', args, {
    cwd: rootDirectory,
    env: environment,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  })
  if (result.error) {
    console.error(`unable to run docker compose: ${result.error.message}`)
    process.exit(1)
  }
  const services = parseComposeJSON(result.stdout)
  const expectedServices = new Set(['postgres-dev', 'mqtt-dev'])
  const healthyServices = new Set(services
    .filter((entry) => entry.State === 'running' && entry.Health === 'healthy')
    .map((entry) => entry.Service))
  const missing = [...expectedServices].filter((service) => !healthyServices.has(service))
  for (const service of expectedServices) {
    const entry = services.find((candidate) => candidate.Service === service)
    console.log(`${service}: ${entry?.State ?? 'missing'} (${entry?.Health ?? 'unknown'})`)
  }
  if (result.status !== 0 || missing.length > 0) {
    console.error(`services are not running and healthy: ${missing.join(', ') || 'unknown'}`)
    process.exit(1)
  }
  process.exit(0)
}

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

function loadDevelopmentCredentials() {
  let rawUrl = process.env.PULSEGRID_DATABASE_URL
  if (rawUrl === undefined) {
    let contents
    try {
      contents = readFileSync(`${rootDirectory}/${dotenvPath}`, 'utf8')
    } catch {
      console.error(`missing ${dotenvPath}; copy apps/api/.env.development.example first`)
      process.exit(1)
    }
    rawUrl = parseDotenv(contents).PULSEGRID_DATABASE_URL
  }
  if (!rawUrl) {
    console.error('PULSEGRID_DATABASE_URL must be set')
    process.exit(1)
  }

  let parsed
  try {
    parsed = new URL(rawUrl)
  } catch {
    console.error(`${dotenvPath} contains an invalid PULSEGRID_DATABASE_URL`)
    process.exit(1)
  }

  if (parsed.hostname !== '127.0.0.1' || parsed.port !== '5432' || parsed.pathname !== '/pulsegrid_dev' || parsed.search !== '?sslmode=disable' || parsed.hash !== '') {
    console.error(`${dotenvPath} must target 127.0.0.1:5432/pulsegrid_dev with sslmode=disable`)
    process.exit(1)
  }
  let username
  let password
  try {
    username = decodeURIComponent(parsed.username)
    password = decodeURIComponent(parsed.password)
  } catch {
    console.error(`${dotenvPath} contains invalid encoded database credentials`)
    process.exit(1)
  }
  if (username !== 'pulsegrid') {
    console.error('PULSEGRID_DATABASE_URL must use the pulsegrid local user')
    process.exit(1)
  }
  if (!password || password === 'CHANGE_ME') {
    console.error('PULSEGRID_DATABASE_URL must contain a disposable database password, not CHANGE_ME')
    process.exit(1)
  }
  return { password }
}

function parseDotenv(contents) {
  const values = {}
  for (const line of contents.split(/\r?\n/u)) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const separator = trimmed.indexOf('=')
    if (separator <= 0) continue
    const key = trimmed.slice(0, separator).trim()
    let value = trimmed.slice(separator + 1).trim()
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1)
    }
    values[key] = value
  }
  return values
}

function parseComposeJSON(output) {
  return output
    .split(/\r?\n/u)
    .filter(Boolean)
    .flatMap((line) => {
      try {
        return [JSON.parse(line)]
      } catch {
        return []
      }
    })
}
