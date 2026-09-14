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
if (!['up', 'stop', 'psql'].includes(operation)) {
  console.error('usage: node scripts/db-dev.mjs <up|stop|psql> [psql args]')
  process.exit(2)
}

const credentials = loadDevelopmentCredentials()
const environment = {
  ...process.env,
  PULSEGRID_POSTGRES_PASSWORD: credentials.password,
}

const args = operation === 'up'
  ? ['compose', '--profile', 'dev', 'up', '-d', '--wait', 'postgres-dev']
  : operation === 'stop'
    ? ['compose', '--profile', 'dev', 'stop', 'postgres-dev']
    : [
        'compose',
        '--profile',
        'dev',
        'exec',
        ...(process.stdin.isTTY ? [] : ['-T']),
        'postgres-dev',
        'psql',
        '-U',
        'pulsegrid',
        '-d',
        'pulsegrid_dev',
        ...operationArguments,
      ]

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
  let contents
  try {
    contents = readFileSync(`${rootDirectory}/${dotenvPath}`, 'utf8')
  } catch {
    console.error(`missing ${dotenvPath}; copy apps/api/.env.development.example first`)
    process.exit(1)
  }

  const values = parseDotenv(contents)
  const rawUrl = values.PULSEGRID_DATABASE_URL
  if (!rawUrl) {
    console.error(`${dotenvPath} must define PULSEGRID_DATABASE_URL`)
    process.exit(1)
  }

  let parsed
  try {
    parsed = new URL(rawUrl)
  } catch {
    console.error(`${dotenvPath} contains an invalid PULSEGRID_DATABASE_URL`)
    process.exit(1)
  }

  if (parsed.hostname !== '127.0.0.1' || parsed.port !== '5432' || parsed.pathname !== '/pulsegrid_dev' || parsed.search !== '?sslmode=disable') {
    console.error(`${dotenvPath} must target 127.0.0.1:5432/pulsegrid_dev with sslmode=disable`)
    process.exit(1)
  }
  if (!parsed.password || parsed.password === 'CHANGE_ME') {
    console.error(`${dotenvPath} must contain a disposable database password, not CHANGE_ME`)
    process.exit(1)
  }
  return { password: parsed.password }
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
