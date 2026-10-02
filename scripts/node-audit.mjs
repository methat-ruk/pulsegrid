import { runRuntimeArtifactGate } from './runtime-artifact-gate.mjs'
import { writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

await runRuntimeArtifactGate().catch(async (error) => {
  console.error(`runtime artifact dependency gate failed closed: ${error.message}`)
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const reportPath = path.resolve(process.env.RUNTIME_AUDIT_REPORT ?? path.join(root, 'runtime-audit-report.json'))
  try {
    await writeFile(reportPath, `${JSON.stringify({ schemaVersion: 1, gate: { passed: false, failures: [error.message] } }, null, 2)}\n`)
  } catch {}
  process.exitCode = 1
})
