import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const root = process.cwd()

function fail(message) {
  console.error(`repository policy: ${message}`)
  process.exitCode = 1
}

function trackedFiles() {
  return execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' })
    .split('\0')
    .filter(Boolean)
}

function checkEnvironmentFiles(files) {
  const disallowed = files.filter((file) => {
    const name = path.basename(file)
    return name === '.env' || (name.startsWith('.env.') && !name.endsWith('.example'))
  })

  if (disallowed.length > 0) {
    fail(`tracked real environment files found: ${disallowed.join(', ')}`)
  }
}

function checkLockfiles(files) {
  const lockfilePattern = /(?:^|\/)(pnpm-lock\.yaml|package-lock\.json|yarn\.lock|npm-shrinkwrap\.json|bun\.lockb?|deno\.lock(?:b)?)$/
  const lockfiles = files.filter((file) => lockfilePattern.test(file))
  if (lockfiles.length !== 1 || lockfiles[0] !== 'pnpm-lock.yaml') {
    fail(`expected exactly one root pnpm lockfile; found ${lockfiles.join(', ') || 'none'}`)
  }
}

function checkToolchainPins() {
  const nodeVersion = readFileSync(path.join(root, '.node-version'), 'utf8').trim()
  const goVersion = readFileSync(path.join(root, '.go-version'), 'utf8').trim()
  const packageJson = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'))
  const packageManager = packageJson.packageManager

  if (!/^\d+\.\d+\.\d+$/.test(nodeVersion)) {
    fail(`.node-version must contain one exact version; found ${JSON.stringify(nodeVersion)}`)
  }
  if (!/^\d+\.\d+\.\d+$/.test(goVersion)) {
    fail(`.go-version must contain one exact version; found ${JSON.stringify(goVersion)}`)
  }
  if (packageManager !== 'pnpm@12.3.4') {
    fail(`packageManager must remain pnpm@12.3.4; found ${JSON.stringify(packageManager)}`)
  }
  if (packageJson.engines?.node !== `>=24.11.0 <25`) {
    fail(`Node engine range changed unexpectedly: ${JSON.stringify(packageJson.engines?.node)}`)
  }
  if (packageJson.engines?.pnpm !== `>=12.3.4 <13`) {
    fail(`pnpm engine range changed unexpectedly: ${JSON.stringify(packageJson.engines?.pnpm)}`)
  }
}

function workflowFiles() {
  const directory = path.join(root, '.github', 'workflows')
  if (!existsSync(directory)) return []
  return readdirSync(directory)
    .filter((file) => file.endsWith('.yml') || file.endsWith('.yaml'))
    .map((file) => path.join(directory, file))
    .filter((file) => statSync(file).isFile())
}

function checkActionPins() {
  const usesPattern = /^\s*uses:\s*([^\s#]+)(?:\s+#\s*(.*))?\s*$/
  const shaPattern = /^[^/@\s]+\/[^/@\s]+@[0-9a-f]{40}$/

  for (const file of workflowFiles()) {
    const relative = path.relative(root, file)
    const lines = readFileSync(file, 'utf8').split(/\r?\n/)
    lines.forEach((line, index) => {
      const match = line.match(usesPattern)
      if (!match || match[1].startsWith('./') || match[1].startsWith('docker://')) return
      if (!shaPattern.test(match[1])) {
        fail(`${relative}:${index + 1} must pin action to a full commit SHA: ${match[1]}`)
      }
      if (!match[2]?.trim()) {
        fail(`${relative}:${index + 1} must retain a release tag comment beside the SHA`)
      }
    })
  }
}

const files = trackedFiles()
checkEnvironmentFiles(files)
checkLockfiles(files)
checkToolchainPins()
checkActionPins()

if (process.exitCode) process.exit(process.exitCode)
