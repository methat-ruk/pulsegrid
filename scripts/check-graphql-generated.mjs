import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'

const generatedPaths = [
  'apps/api/graph/generated.go',
  'apps/api/graph/model/models_gen.go',
  'apps/api/graph/device.resolvers.go',
]
const beforeGeneration = new Map(generatedPaths.map((path) => [path, readFileSync(path)]))

const generation = spawnSync('go', ['-C', 'apps/api', 'generate', './graph'], {
  cwd: process.cwd(),
  stdio: 'inherit',
})
if (generation.error) {
  console.error(`unable to run GraphQL generation: ${generation.error.message}`)
  process.exit(1)
}
if (generation.status !== 0) process.exit(generation.status ?? 1)

const stalePaths = generatedPaths.filter((path) => !beforeGeneration.get(path).equals(readFileSync(path)))
if (stalePaths.length > 0) {
  console.error(`GraphQL generated files are stale; regenerate and review: ${stalePaths.join(', ')}`)
  process.exit(1)
}
