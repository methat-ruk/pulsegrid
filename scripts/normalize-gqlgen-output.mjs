import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// gqlgen emits map-copy loops and modernize skips fixes inside generated files.
// Normalize its output so code generation stays compatible with the Go checks.
const generatedPath = fileURLToPath(new URL('../apps/api/graph/generated.go', import.meta.url))
const original = readFileSync(generatedPath, 'utf8')
const generatedLoop = '\tfor k, v := range obj.(map[string]any) {\n\t\tasMap[k] = v\n\t}'
const mapsCopy = '\tmaps.Copy(asMap, obj.(map[string]any))'
const replacements = original.split(generatedLoop).length - 1

if (replacements === 0 && !original.includes(mapsCopy)) {
  throw new Error('gqlgen input map-copy template changed; review the generated output before updating this normalizer')
}

let normalized = original.replaceAll(generatedLoop, mapsCopy)
if (!normalized.includes('\t"maps"\n')) {
  const importAnchor = '\t"math"\n'
  if (!normalized.includes(importAnchor)) {
    throw new Error('gqlgen generated import block changed; cannot add the maps import safely')
  }
  normalized = normalized.replace(importAnchor, `\t"maps"\n${importAnchor}`)
}

if (normalized.includes(generatedLoop)) {
  throw new Error('gqlgen map-copy loops remain after normalization')
}

if (normalized !== original) writeFileSync(generatedPath, normalized)
