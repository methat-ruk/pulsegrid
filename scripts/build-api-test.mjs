import { mkdirSync } from 'node:fs'
import { spawnSync } from 'node:child_process'

mkdirSync('.output', { recursive: true })

const result = spawnSync('go', [
  '-C',
  'apps/api',
  'build',
  '-o',
  '../../.output/api-test',
  './cmd/api',
], { stdio: 'inherit' })

if (result.error) {
  console.error(`unable to build API test binary: ${result.error.message}`)
  process.exit(1)
}
process.exit(result.status ?? 1)
