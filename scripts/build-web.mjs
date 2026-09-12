import { spawnSync } from 'node:child_process'
import process from 'node:process'

const environment = process.argv[2]
if (!['test', 'production'].includes(environment)) {
  console.error('usage: node scripts/build-web.mjs <test|production>')
  process.exit(2)
}

const result = spawnSync('pnpm', ['--filter', '@pulsegrid/web-console', 'build'], {
  env: {
    ...process.env,
    NUXT_APP_ENV: environment,
    NUXT_TELEMETRY_DISABLED: '1',
  },
  stdio: 'inherit',
})

if (result.error) {
  console.error(`unable to run frontend build: ${result.error.message}`)
  process.exit(1)
}
process.exit(result.status ?? 1)
