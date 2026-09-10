import { spawnSync } from 'node:child_process'

const result = spawnSync('pnpm', ['audit', '--prod', '--json'], {
  encoding: 'utf8',
})

if (result.error) {
  console.error(`unable to run pnpm audit: ${result.error.message}`)
  process.exit(1)
}

const output = result.stdout.trim()
let report
try {
  report = JSON.parse(output)
} catch {
  console.error('pnpm audit did not return a JSON report; dependency evidence is incomplete')
  if (result.stderr) process.stderr.write(result.stderr)
  if (output) process.stdout.write(`${output}\n`)
  process.exit(1)
}

const vulnerabilities = report?.metadata?.vulnerabilities
const severityNames = ['info', 'low', 'moderate', 'high', 'critical']
const isAuditReport = report && typeof report === 'object' && !Array.isArray(report)
  && report.advisories && typeof report.advisories === 'object' && !Array.isArray(report.advisories)
  && vulnerabilities && typeof vulnerabilities === 'object' && !Array.isArray(vulnerabilities)
  && severityNames.every((severity) => Number.isInteger(vulnerabilities[severity]))

if (!isAuditReport) {
  console.error('pnpm audit returned an incomplete or error-shaped JSON report; dependency evidence is incomplete')
  if (result.stderr) process.stderr.write(result.stderr)
  process.exit(1)
}

const advisories = Object.values(report.advisories)
const severityRank = { info: 0, low: 1, moderate: 2, high: 3, critical: 4 }

if (result.signal || (result.status !== 0 && !(result.status === 1 && advisories.length > 0))) {
  const status = result.signal ? `signal ${result.signal}` : `exit code ${result.status}`
  console.error(`pnpm audit failed with ${status}; dependency evidence is incomplete`)
  if (result.stderr) process.stderr.write(result.stderr)
  process.exit(1)
}

if (advisories.length === 0) {
  console.log('pnpm audit --prod: no advisories found')
  process.exit(0)
}

console.log(`pnpm audit --prod: ${advisories.length} advisory(ies) found`)
for (const advisory of advisories) {
  const severity = advisory.severity ?? 'unknown'
  console.log(`- ${severity}: ${advisory.module_name}: ${advisory.title}`)
  if (advisory.url) console.log(`  ${advisory.url}`)
}

const blocking = advisories.filter((advisory) => (severityRank[advisory.severity] ?? 4) >= severityRank.moderate)
if (blocking.length > 0) {
  console.error(`${blocking.length} moderate-or-higher production advisory(ies) block the gate`)
  process.exit(1)
}

console.log('No moderate, high, or critical production advisories; low/info findings remain visible for disposition.')
