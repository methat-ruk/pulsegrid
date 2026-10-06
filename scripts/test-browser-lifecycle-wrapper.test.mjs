import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { chmod, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import net from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

const lifecyclePath = join(process.cwd(), 'scripts', 'test-browser-lifecycle.mjs')
const ownedPorts = [15432, 11883, 18080, 4173]

for (const signal of ['SIGINT', 'SIGTERM']) {
  test(`cleans the owned runner and permits a rerun when the lifecycle wrapper receives ${signal}`, async (t) => {
    if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
    if (await anyPortOpen(ownedPorts)) return t.skip('a browser lifecycle port is already in use')
    const fixture = await createFixture(t)
    const wrapper = startLifecycleWrapper(fixture, 'hold')
    await waitForFile(fixture.pnpmStarted, 8_000)
    const runnerPID = Number(await readFile(fixture.pnpmPID, 'utf8'))

    wrapper.child.kill(signal)
    const interrupted = await waitForClose(wrapper, 20_000)
    assert.equal(interrupted.code, signal === 'SIGINT' ? 130 : 143, wrapper.output)
    assert.equal(interrupted.signal, null)
    await waitForProcessExit(runnerPID, 5_000)
    await waitForOwnedPortsClosed()
    assert.deepEqual(await readOwnedResources(fixture), [])
    assert.deepEqual((await readFile(fixture.buildCallsPath, 'utf8')).trim().split('\n'), ['api', 'web'])

    const rerun = startLifecycleWrapper(fixture, 'complete')
    const completed = await waitForClose(rerun, 20_000)
    assert.equal(completed.code, 0, rerun.output)
    assert.equal(completed.signal, null)
    await waitForOwnedPortsClosed()
    assert.deepEqual(await readOwnedResources(fixture), [])
    assert.deepEqual((await readFile(fixture.buildCallsPath, 'utf8')).trim().split('\n'), ['api', 'web', 'api', 'web'])
  })
}

test('fails closed when the lifecycle wrapper cannot remove its Compose resources', async (t) => {
  if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
  if (await anyPortOpen(ownedPorts)) return t.skip('a browser lifecycle port is already in use')
  const fixture = await createFixture(t)
  const wrapper = startLifecycleWrapper(fixture, 'fail-wrapper-down')
  const result = await waitForClose(wrapper, 20_000)

  assert.equal(result.code, 1, wrapper.output)
  assert.equal(result.signal, null)
  assert.match(wrapper.output, /Docker down exited 29/u)
  assert.deepEqual(await readOwnedResources(fixture), [])
})

test('reuses browser-suite artifacts across lifecycle scenarios when requested', async (t) => {
  if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
  if (await anyPortOpen(ownedPorts)) return t.skip('a browser lifecycle port is already in use')
  const fixture = await createFixture(t)
  const wrapper = startLifecycleWrapper(fixture, 'complete', { reuseTestArtifacts: true })
  const result = await waitForClose(wrapper, 20_000)

  assert.equal(result.code, 0, wrapper.output)
  assert.equal(result.signal, null)
  await assert.rejects(readFile(fixture.buildCallsPath, 'utf8'), { code: 'ENOENT' })
  assert.deepEqual(await readOwnedResources(fixture), [])
})

test('bounds a hanging Docker inspection and terminates its process group', async (t) => {
  if (process.platform === 'win32') return t.skip('process-group signals require POSIX')
  if (await anyPortOpen(ownedPorts)) return t.skip('a browser lifecycle port is already in use')
  const fixture = await createFixture(t)
  const wrapper = startLifecycleWrapper(fixture, 'hang-network-inspection')
  const startedAt = Date.now()
  const result = await waitForClose(wrapper, 25_000)

  assert.equal(result.code, 1, wrapper.output)
  assert.equal(result.signal, null)
  assert.match(wrapper.output, /timed out after 15000ms/u)
  const dockerPID = Number(await readFile(fixture.hangingDockerPID, 'utf8'))
  await waitForProcessExit(dockerPID, 5_000)
  assert.ok(Date.now() - startedAt < 22_000, 'Docker inspection must have a bounded runtime')
  await waitForOwnedPortsClosed()
  assert.deepEqual(await readOwnedResources(fixture), [])
})

async function createFixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'pulsegrid-lifecycle-wrapper-'))
  const dockerPath = join(directory, 'docker')
  const goPath = join(directory, 'go')
  const nodePath = join(directory, 'node')
  const pnpmPath = join(directory, 'pnpm')
  const pnpmStarted = join(directory, 'pnpm-started')
  const pnpmPID = join(directory, 'pnpm-pid')
  const hangingDockerPID = join(directory, 'hanging-docker-pid')
  const callsPath = join(directory, 'docker-calls')
  const buildCallsPath = join(directory, 'build-calls')
  const projectsPath = join(directory, 'projects')
  await mkdir(projectsPath, { recursive: true })

  await writeFile(dockerPath, `#!${process.execPath}
import { appendFileSync, existsSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
const args = process.argv.slice(2)
const projectIndex = args.indexOf('-p')
const project = projectIndex >= 0 ? args[projectIndex + 1] : 'missing-project'
const resourcePath = join(process.env.MVP013_LIFECYCLE_PROJECTS, project + '.resource')
const brokerPIDPath = join(process.env.MVP013_LIFECYCLE_PROJECTS, project + '.broker-pid')
appendFileSync(process.env.MVP013_LIFECYCLE_DOCKER_CALLS, args.join(' ') + '\\n')
if (args.includes('up')) {
  writeFileSync(resourcePath, 'owned')
  const broker = spawn(process.execPath, ['-e', 'const net = require("node:net"); net.createServer().listen(11883, "127.0.0.1"); setInterval(() => {}, 1000)'], { detached: true, stdio: 'ignore' })
  broker.unref()
  writeFileSync(brokerPIDPath, String(broker.pid))
  process.stdout.write('Network ' + project + '_default Created\\n')
}
else if (args.includes('down')) {
  const countPath = join(process.env.MVP013_LIFECYCLE_PROJECTS, project + '.down-count')
  const count = Number(existsSync(countPath) ? readFileSync(countPath, 'utf8') : '0') + 1
  writeFileSync(countPath, String(count))
  if (existsSync(brokerPIDPath)) {
    try { process.kill(Number(readFileSync(brokerPIDPath, 'utf8')), 'SIGTERM') } catch (error) { if (error.code !== 'ESRCH') throw error }
    unlinkSync(brokerPIDPath)
  }
  try { unlinkSync(resourcePath) } catch (error) { if (error.code !== 'ENOENT') throw error }
  if (process.env.MVP013_LIFECYCLE_FAKE_MODE === 'fail-wrapper-down' && count >= 2) {
    process.stderr.write('injected final Compose down failure\\n')
    process.exitCode = 29
  }
}
else if (args[0] === 'network' && args.includes('ls') && process.env.MVP013_LIFECYCLE_FAKE_MODE === 'hang-network-inspection') {
  writeFileSync(process.env.MVP013_LIFECYCLE_HANGING_DOCKER_PID, String(process.pid))
  process.on('SIGTERM', () => {})
  setInterval(() => {}, 1000)
}
`)
  await writeFile(goPath, `#!${process.execPath}\nprocess.exitCode = 0\n`)
  await writeFile(nodePath, `#!${process.execPath}
import { appendFileSync } from 'node:fs'
import net from 'node:net'
import { spawn } from 'node:child_process'
if (process.argv[2] === 'scripts/build-api-test.mjs') appendFileSync(process.env.MVP013_LIFECYCLE_BUILD_CALLS, 'api\\n')
else if (process.argv[2] === 'scripts/build-web.mjs') appendFileSync(process.env.MVP013_LIFECYCLE_BUILD_CALLS, 'web\\n')
else if (process.argv[2] === 'scripts/start-nuxt-test-server.mjs') {
  const source = 'const http = require("node:http"); http.createServer((req, res) => { res.setHeader("content-type", "application/json"); res.end(JSON.stringify({status: "ready"})) }).listen(18080, "127.0.0.1"); setInterval(() => {}, 1000)'
  spawn(process.execPath, ['-e', source], { stdio: 'ignore' })
  net.createServer().listen(4173, '127.0.0.1')
  setInterval(() => {}, 1000)
}
`)
  await writeFile(pnpmPath, `#!${process.execPath}
import { writeFileSync } from 'node:fs'
if (process.env.MVP013_LIFECYCLE_FAKE_MODE === 'hold') {
  writeFileSync(process.env.MVP013_LIFECYCLE_PNPM_STARTED, 'ready')
  writeFileSync(process.env.MVP013_LIFECYCLE_PNPM_PID, String(process.pid))
  process.on('SIGTERM', () => {})
  setInterval(() => {}, 1000)
}
else setTimeout(() => process.stdout.write('1 passed (simulated product-loop journey)\\n'), 1500)
`)
  for (const path of [dockerPath, goPath, nodePath, pnpmPath]) await chmod(path, 0o755)
  t.after(async () => {
    await stopFixtureProcesses(projectsPath)
    try { process.kill(Number(await readFile(hangingDockerPID, 'utf8')), 'SIGKILL') } catch (error) { if (error.code !== 'ENOENT' && error.code !== 'ESRCH') throw error }
    await rm(directory, { recursive: true, force: true })
  })
  return { directory, pnpmStarted, pnpmPID, hangingDockerPID, callsPath, buildCallsPath, projectsPath }
}

function startLifecycleWrapper(fixture, mode, { reuseTestArtifacts = false } = {}) {
  const child = spawn(process.execPath, [lifecyclePath, ...(reuseTestArtifacts ? ['--reuse-test-artifacts'] : [])], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      PATH: `${fixture.directory}${process.platform === 'win32' ? ';' : ':'}${process.env.PATH}`,
      MVP013_LIFECYCLE_FAKE_MODE: mode,
      MVP013_LIFECYCLE_DOCKER_CALLS: fixture.callsPath,
      MVP013_LIFECYCLE_PROJECTS: fixture.projectsPath,
      MVP013_LIFECYCLE_PNPM_STARTED: fixture.pnpmStarted,
      MVP013_LIFECYCLE_PNPM_PID: fixture.pnpmPID,
      MVP013_LIFECYCLE_HANGING_DOCKER_PID: fixture.hangingDockerPID,
      MVP013_LIFECYCLE_BUILD_CALLS: fixture.buildCallsPath,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const wrapper = { child, output: '', closed: undefined }
  const append = chunk => { wrapper.output = `${wrapper.output}${chunk.toString()}`.slice(-96 * 1024) }
  child.stdout.on('data', append)
  child.stderr.on('data', append)
  wrapper.closed = new Promise((resolve) => {
    child.once('close', (code, signal) => resolve({ code, signal }))
  })
  return wrapper
}

async function waitForClose(wrapper, timeoutMilliseconds) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`lifecycle wrapper did not exit within ${timeoutMilliseconds}ms\\n${wrapper.output}`)), timeoutMilliseconds)
  })
  try { return await Promise.race([wrapper.closed, timeout]) }
  finally { clearTimeout(timer) }
}

async function waitForFile(path, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    try { await readFile(path); return }
    catch (error) { if (error.code !== 'ENOENT') throw error }
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  throw new Error(`fixture marker ${path} was not written`)
}

async function waitForProcessExit(pid, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    try { process.kill(pid, 0) }
    catch (error) { if (error.code === 'ESRCH') return; throw error }
    await new Promise(resolve => setTimeout(resolve, 25))
  }
  throw new Error(`owned process ${pid} remained alive after ${timeoutMilliseconds}ms`)
}

async function waitForOwnedPortsClosed() {
  const deadline = Date.now() + 5_000
  while (Date.now() < deadline) {
    if (!(await anyPortOpen(ownedPorts))) return
    await new Promise(resolve => setTimeout(resolve, 50))
  }
  assert.deepEqual(await openPorts(ownedPorts), [], 'owned ports should close after wrapper cancellation')
}

async function anyPortOpen(ports) {
  return (await openPorts(ports)).length > 0
}

async function openPorts(ports) {
  const open = []
  for (const port of ports) {
    const server = net.createConnection({ host: '127.0.0.1', port })
    const listening = await new Promise((resolve) => {
      const finish = (value) => { server.destroy(); resolve(value) }
      server.once('connect', () => finish(true))
      server.once('error', () => finish(false))
      server.setTimeout(200, () => finish(false))
    })
    if (listening) open.push(port)
  }
  return open
}

async function readOwnedResources(fixture) {
  return (await readdir(fixture.projectsPath)).filter(entry => entry.endsWith('.resource')).sort()
}

async function stopFixtureProcesses(projectsPath) {
  let files
  try { files = await readdir(projectsPath) } catch (error) { if (error.code === 'ENOENT') return; throw error }
  for (const file of files.filter(name => name.endsWith('.broker-pid'))) {
    try { process.kill(Number(await readFile(join(projectsPath, file), 'utf8')), 'SIGKILL') }
    catch (error) { if (error.code !== 'ESRCH') throw error }
  }
}
