import { spawnSync } from 'node:child_process'

const environment = {
  ...process.env,
  NUXT_APP_ENV: 'test',
  NUXT_BACKEND_ORIGIN: 'http://127.0.0.1:18080',
  NUXT_TELEMETRY_DISABLED: '1',
}

const commands = [
  ['node', ['scripts/build-api-test.mjs']],
  ['node', ['scripts/build-web.mjs', 'test']],
  ['pnpm', ['exec', 'playwright', 'test']],
]

for (const [command, args] of commands) {
  const result = spawnSync(command, args, { env: environment, stdio: 'inherit' })
  if (result.error) {
    console.error(`unable to run ${command}: ${result.error.message}`)
    process.exit(1)
  }
  if (result.status !== 0) process.exit(result.status ?? 1)
}
