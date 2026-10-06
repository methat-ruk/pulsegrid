import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { chmod, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
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
  assert.deepEqual(await readOwnedResources(fixture), [])
})

test('cleans descendants when the Compose leader exits before its process group', async (t) => {
  if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
  const fixture = await createDockerFixture(t)
  const result = await runBrowserRunner(fixture, {
    CI: 'true',
    PULSEGRID_DATABASE_URL: externalTestDatabase,
    PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
    MVP013_BROWSER_FAKE_DOCKER_MODE: 'leader-exits-with-descendant',
  })

  assert.equal(result.code, 0, result.output)
  assert.match(result.output, /owned descendant process remained; cleaning up its process group/iu)
  const descendantPID = Number(await readFile(fixture.descendantPID, 'utf8'))
  await waitForProcessExit(descendantPID, 5_000)
  assert.deepEqual((await readDockerCalls(fixture.marker)).trim().split('\n'), ['up', 'down'])
  assert.deepEqual(await readOwnedResources(fixture), [])
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  test(`cleans owned resources and permits a rerun after ${signal} at browser readiness`, async (t) => {
    if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
    const fixture = await createDockerFixture(t)
    const child = spawn(process.execPath, [runnerPath], {
      cwd: process.cwd(),
      env: runnerEnvironment(fixture, {
        CI: 'true',
        PULSEGRID_DATABASE_URL: externalTestDatabase,
        PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
        MVP013_BROWSER_FAKE_PNPM_MODE: 'hold-after-readiness',
      }),
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let output = ''
    child.stdout.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })
    child.stderr.on('data', chunk => { output = `${output}${chunk.toString()}`.slice(-64 * 1024) })
    try {
      await waitForMarker(fixture.readinessMarker, 'ready', 8_000)
    }
    catch (error) {
      child.kill('SIGKILL')
      throw new Error(`${error.message}\n${output}`)
    }
    const descendantPID = Number(await readFile(fixture.descendantPID, 'utf8'))

    child.kill(signal)
    const [code, receivedSignal] = await waitForChildClose(child, 12_000, () => output)
    assert.equal(code, signal === 'SIGINT' ? 130 : 143)
    assert.equal(receivedSignal, null)
    await waitForProcessExit(descendantPID, 5_000)
    assert.deepEqual((await readDockerCalls(fixture.marker)).trim().split('\n'), ['up', 'down'])
    assert.deepEqual(await readOwnedResources(fixture), [])

    const rerun = await runBrowserRunner(fixture, {
      CI: 'true',
      PULSEGRID_DATABASE_URL: externalTestDatabase,
      PULSEGRID_BROWSER_TEST_DATABASE_MODE: 'external-ci-service',
      MVP013_BROWSER_FAKE_PNPM_MODE: 'complete',
    })
    assert.equal(rerun.code, 0, rerun.output)
    assert.deepEqual((await readDockerCalls(fixture.marker)).trim().split('\n'), ['up', 'down', 'up', 'down'])
    assert.deepEqual(await readOwnedResources(fixture), [])
  })
}

test('reaps an interrupted Compose startup before exiting', async (t) => {
  if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
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
  assert.deepEqual(await readOwnedResources(fixture), [])
})

async function createDockerFixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'pulsegrid-browser-runner-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const marker = join(directory, 'docker-calls.txt')
  const dockerPath = join(directory, 'docker')
  const descendantPID = join(directory, 'descendant-pid.txt')
  const readinessMarker = join(directory, 'browser-readiness.txt')
  const fakeDocker = `#!${process.execPath}
import { appendFileSync, unlinkSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { spawn } from 'node:child_process'
const args = process.argv.slice(2)
const projectIndex = args.indexOf('-p')
const projectName = projectIndex >= 0 ? args[projectIndex + 1] : 'missing-project'
const resourcePath = join(dirname(process.env.MVP013_BROWSER_FAKE_DOCKER_LOG), projectName + '.resource')
if (args.includes('up')) {
  appendFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_LOG, 'up\\n')
  writeFileSync(resourcePath, 'owned')
  if (process.env.MVP013_BROWSER_FAKE_DOCKER_MODE === 'wait-up') setInterval(() => {}, 1000)
  if (process.env.MVP013_BROWSER_FAKE_DOCKER_MODE === 'leader-exits-with-descendant') {
    const descendant = spawn(process.execPath, ['-e', 'process.on("SIGTERM", () => {}); setInterval(() => {}, 1000)'], { stdio: 'ignore' })
    descendant.unref()
    writeFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_DESCENDANT_PID, String(descendant.pid))
    process.exit(0)
  }
  process.exitCode = process.env.MVP013_BROWSER_FAKE_DOCKER_MODE === 'fail-up' ? 29 : 0
}
else if (args.includes('down')) {
  appendFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_LOG, 'down\\n')
  try { unlinkSync(resourcePath) }
  catch (error) { if (error.code !== 'ENOENT') throw error }
}
else {
  process.exitCode = 31
}
`
  await writeFile(dockerPath, fakeDocker, 'utf8')
  await chmod(dockerPath, 0o755)
  const fakeGoPath = join(directory, 'go')
  await writeFile(fakeGoPath, `#!${process.execPath}\nprocess.exitCode = 0\n`, 'utf8')
  await chmod(fakeGoPath, 0o755)
  const fakeNodePath = join(directory, 'node')
  const fakeNode = `#!${process.execPath}
import net from 'node:net'
if (process.argv[2] === 'scripts/start-nuxt-test-server.mjs') {
  net.createServer().listen(4173, '127.0.0.1')
  setInterval(() => {}, 1000)
}
`
  await writeFile(fakeNodePath, fakeNode, 'utf8')
  await chmod(fakeNodePath, 0o755)
  const fakePnpmPath = join(directory, 'pnpm')
  const fakePnpm = `#!${process.execPath}
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
if (process.env.MVP013_BROWSER_FAKE_PNPM_MODE === 'hold-after-readiness') {
  const descendant = spawn(process.execPath, ['-e', 'process.on("SIGTERM", () => {}); setInterval(() => {}, 1000)'], { stdio: 'ignore' })
  descendant.unref()
  writeFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_DESCENDANT_PID, String(descendant.pid))
  writeFileSync(process.env.MVP013_BROWSER_FAKE_DOCKER_READY, 'ready')
  setInterval(() => {}, 1000)
}
else {
  process.stdout.write('1 passed (simulated browser-runner test)\\n')
}
`
  await writeFile(fakePnpmPath, fakePnpm, 'utf8')
  await chmod(fakePnpmPath, 0o755)
  return { directory, marker, descendantPID, readinessMarker }
}

function runnerEnvironment(fixture, values) {
  return {
    ...process.env,
    ...values,
    PATH: `${fixture.directory}${process.platform === 'win32' ? ';' : ':'}${process.env.PATH}`,
    MVP013_BROWSER_FAKE_DOCKER_LOG: fixture.marker,
    MVP013_BROWSER_FAKE_DOCKER_DESCENDANT_PID: fixture.descendantPID,
    MVP013_BROWSER_FAKE_DOCKER_READY: fixture.readinessMarker,
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

async function readOwnedResources(fixture) {
  const entries = await readdir(fixture.directory)
  return entries.filter(entry => entry.endsWith('.resource')).sort()
}

async function waitForProcessExit(pid, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0)
    }
    catch (error) {
      if (error.code === 'ESRCH') return
      throw error
    }
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  throw new Error(`descendant process ${pid} remained alive after ${timeoutMilliseconds}ms`)
}

async function waitForMarker(marker, expected, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    if ((await readDockerCalls(marker)).trim().split('\n').includes(expected)) return
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  throw new Error(`Docker fixture did not record ${expected}`)
}
