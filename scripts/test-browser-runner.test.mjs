import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import net from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

const runnerPath = join(process.cwd(), 'scripts', 'test-browser.mjs')
const externalTestDatabase = 'postgres://pulsegrid:runner-test-password@127.0.0.1:15432/pulsegrid_test?sslmode=disable'

test('rejects an unsafe external database URL before starting or tearing down resources', async (t) => {
  const fixture = await createDockerFixture(t)
  const result = await runBrowserRunner(fixture, {
    CI: 'true',
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
    PULSEGRID_DATABASE_URL: 'postgres://pulsegrid:private-test-password@localhost:5432/production?sslmode=disable',
  })

  assert.notEqual(result.code, 0)
  assert.match(result.output, /refusing browser smoke tests: external database target/iu)
  assert.equal(await readDockerCalls(fixture.marker), '')
  assert.doesNotMatch(result.output, /private-test-password|production/iu)
})

test('does not allow local shells to opt into the CI-owned database', async (t) => {
  const fixture = await createDockerFixture(t)
  const result = await runBrowserRunner(fixture, {
    CI: 'false',
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
    PULSEGRID_DATABASE_URL: externalTestDatabase,
  })

  assert.notEqual(result.code, 0)
  assert.match(result.output, /external database mode is CI-only/iu)
  assert.equal(await readDockerCalls(fixture.marker), '')
})

test('requires an explicit test database URL in CI-owned mode', async (t) => {
  const fixture = await createDockerFixture(t)
  const result = await runBrowserRunner(fixture, {
    CI: 'true',
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
  })

  assert.notEqual(result.code, 0)
  assert.match(result.output, /external database target must be the isolated loopback test database/iu)
  assert.equal(await readDockerCalls(fixture.marker), '')
})

test('leaves an occupied owned port alone and skips Compose teardown', async (t) => {
  const fixture = await createDockerFixture(t)
  const server = net.createServer()
  try {
    await new Promise((resolve, reject) => {
      server.once('error', reject)
      server.listen(4173, '127.0.0.1', resolve)
    })
  }
  catch (error) {
    if (error.code === 'EADDRINUSE') return t.skip('browser port 4173 is already owned by another process')
    throw error
  }
  t.after(() => new Promise(resolve => server.close(resolve)))

  const result = await runBrowserRunner(fixture, {
    CI: 'true',
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
    PULSEGRID_DATABASE_URL: externalTestDatabase,
  })
  assert.notEqual(result.code, 0)
  assert.match(result.output, /web=127\.0\.0\.1:4173/u)
  assert.equal(server.listening, true)
  assert.equal(await readDockerCalls(fixture.marker), '')
})

test('cleans the unique Compose project after startup partially fails', async (t) => {
  const fixture = await createDockerFixture(t)
  const result = await runBrowserRunner(fixture, {
    CI: 'true',
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
    PULSEGRID_DATABASE_URL: externalTestDatabase,
    MVP013_BROWSER_FAKE_DOCKER_MODE: 'fail-up',
  })

  assert.notEqual(result.code, 0)
  assert.deepEqual((await readDockerCalls(fixture.marker)).trim().split('\n'), ['up', 'down'])
})

test('reaps an interrupted Compose startup before exiting', async (t) => {
  const fixture = await createDockerFixture(t)
  const child = spawn(process.execPath, [runnerPath], {
    cwd: process.cwd(),
    env: runnerEnvironment(fixture, {
      CI: 'true',
      PULSEGRID_DATABASE_URL: externalTestDatabase,
      PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
      MVP013_BROWSER_FAKE_DOCKER_MODE: 'wait-up',
    }),
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let output = ''
  child.stdout.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })
  child.stderr.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })

  await waitForMarker(fixture.marker, 'up', 5_000)
  child.kill('SIGTERM')
  const [code, signal] = await waitForChildClose(child, 12_000, () => output)

  assert.equal(code, 143)
  assert.equal(signal, null)
  assert.deepEqual((await readDockerCalls(fixture.marker)).trim().split('\n'), ['up', 'down'])
})

async function createDockerFixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'pulsegrid-browser-runner-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const marker = join(directory, 'docker-calls.txt')
  const dockerPath = join(directory, 'docker')
  const fakeDocker = `#!/usr/bin/env node
import { appendFileSync } from 'node:fs'
const args = process.argv.slice(2)
if (args.includes('up')) {
  appendFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_LOG, 'up\\n')
  if (process.env.MVP013_BROWSER_FAKE_DOCKER_MODE === 'wait-up') setInterval(() => {}, 1000)
  process.exitCode = process.env.MVP013_BROWSER_FAKE_DOCKER_MODE === 'fail-up' ? 29 : 0
}
else if (args.includes('down')) {
  appendFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_LOG, 'down\\n')
}
else {
  process.exitCode = 31
}
`
  await writeFile(dockerPath, fakeDocker, 'utf8')
  await chmod(dockerPath, 0o755)
  return { directory, marker }
}

function runnerEnvironment(fixture, values) {
  return {
    ...process.env,
    ...values,
    PATH: `${fixture.directory}:${process.env.PATH}`,
    MVP013_BROWSER_FAKE_DOCKER_LOG: fixture.marker,
  }
}

async function runBrowserRunner(fixture, values) {
  const child = spawn(process.execPath, [runnerPath], {
    cwd: process.cwd(),
    env: runnerEnvironment(fixture, values),
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let output = ''
  child.stdout.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })
  child.stderr.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })
  try {
    const [code, signal] = await waitForChildClose(child, 12_000, () => output)
    return { code, signal, output }
  }
  catch (error) {
    child.kill('SIGKILL')
    throw error
  }
}

async function waitForChildClose(child, timeoutMilliseconds, readOutput) {
  let timer
  const closed = once(child, 'close')
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`browser runner did not exit: ${readOutput()}`)), timeoutMilliseconds)
  })
  try {
    return await Promise.race([closed, timeout])
  }
  finally {
    clearTimeout(timer)
  }
}

async function readDockerCalls(marker) {
  try {
    return await readFile(marker, 'utf8')
  }
  catch (error) {
    if (error.code === 'ENOENT') return ''
    throw error
  }
}

async function waitForMarker(marker, expected, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    if ((await readDockerCalls(marker)).trim().split('\n').includes(expected)) return
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  throw new Error(`Docker fixture did not record ${expected}`)
}
