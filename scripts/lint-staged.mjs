import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const files = process.argv
  .slice(2)
  .filter(Boolean)
  .map((file) => path.relative(process.cwd(), path.resolve(file)))
const existingFiles = files.filter((file) => existsSync(path.resolve(file)))
const goFiles = existingFiles.filter((file) => file.endsWith('.go'))
const webFiles = existingFiles.filter((file) => (
  file.startsWith('apps/web-console/') && /\.(?:ts|vue|mjs|js|css)$/.test(file)
))
const openapiFiles = existingFiles.filter((file) => (
  file.startsWith('apps/api/api/openapi/') && /\.(?:yaml|yml)$/.test(file)
))
const openapiConfigChanged = files.includes('redocly.yaml')

function run(command, args) {
  const result = spawnSync(command, args, { stdio: 'inherit' })
  if (result.error) {
    console.error(`lint-staged: unable to run ${command}: ${result.error.message}`)
    process.exit(1)
  }
  if (result.status !== 0) process.exit(result.status ?? 1)
}

if (goFiles.length > 0) run('gofmt', ['-w', ...goFiles])

if (webFiles.length > 0) {
  const relativeWebFiles = webFiles
    .map((file) => path.relative('apps/web-console', file))
    .filter((file) => file && !file.startsWith('..'))
  if (relativeWebFiles.length > 0) {
    run('pnpm', ['--filter', '@pulsegrid/web-console', 'exec', 'eslint', '--fix', ...relativeWebFiles])
  }
}

if (openapiFiles.length > 0 || openapiConfigChanged) {
  run('pnpm', ['exec', 'redocly', 'lint', '--config', 'redocly.yaml', 'apps/api/api/openapi/operational.yaml'])
}

run(process.execPath, [fileURLToPath(new URL('./check-repository-policy.mjs', import.meta.url))])
