import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'

const files = execFileSync('git', [
  'ls-files',
  '-z',
  '--cached',
  '--others',
  '--exclude-standard',
  '--',
  '*.go',
], {
  encoding: 'utf8',
})
  .split('\0')
  .filter(Boolean)
  .filter((file) => existsSync(file))

if (files.length === 0) {
  process.exit(0)
}

const write = process.argv.includes('--write')
const result = spawnSync('gofmt', [write ? '-w' : '-l', ...files], {
  encoding: 'utf8',
})

if (result.error) {
  console.error(`unable to run gofmt: ${result.error.message}`)
  process.exit(1)
}

if (result.status !== 0) {
  if (result.stderr) process.stderr.write(result.stderr)
  process.exit(result.status ?? 1)
}

if (!write && result.stdout.trim()) {
  console.error('Go files need formatting:')
  process.stdout.write(result.stdout)
  process.exit(1)
}
