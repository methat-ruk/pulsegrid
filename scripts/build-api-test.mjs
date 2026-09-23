import { mkdirSync } from 'node:fs'
import { spawnSync } from 'node:child_process'

mkdirSync('.output', { recursive: true })

const commands = [
  ['api-test', './cmd/api'],
  ['device-simulator-test', './cmd/device-simulator'],
]

for (const [name, target] of commands) {
  const result = spawnSync('go', [
    '-C',
    'apps/api',
    'build',
    '-o',
    `../../.output/${name}`,
    target,
  ], { stdio: 'inherit' })

  if (result.error) {
    console.error(`unable to build ${name} binary: ${result.error.message}`)
    process.exit(1)
  }
  if (result.status !== 0) process.exit(result.status ?? 1)
}
