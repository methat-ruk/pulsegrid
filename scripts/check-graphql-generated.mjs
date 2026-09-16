import { spawnSync } from 'node:child_process'

const generatedPaths = [
  'apps/api/graph/generated.go',
  'apps/api/graph/model/models_gen.go',
  'apps/api/graph/device.resolvers.go',
]

const generation = spawnSync('go', ['-C', 'apps/api', 'generate', './graph'], {
  cwd: process.cwd(),
  stdio: 'inherit',
})
if (generation.error) {
  console.error(`unable to run GraphQL generation: ${generation.error.message}`)
  process.exit(1)
}
if (generation.status !== 0) process.exit(generation.status ?? 1)

const diff = spawnSync('git', ['diff', '--exit-code', '--', ...generatedPaths], {
  cwd: process.cwd(),
  stdio: 'inherit',
})
if (diff.error) {
  console.error(`unable to inspect generated GraphQL files: ${diff.error.message}`)
  process.exit(1)
}
if (diff.status !== 0) {
  console.error('GraphQL generated files are stale; run `corepack pnpm run api:generate` and commit the result')
  process.exit(diff.status ?? 1)
}
