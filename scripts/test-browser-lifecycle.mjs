import { spawn } from 'node:child_process'
import net from 'node:net'
import { join } from 'node:path'

const ports = { database: 15432, broker: 11883, api: 18080, web: 4173 }
const externalDatabaseMode = process.env.CI === 'true'
  && process.env.PULSEGRID_BROWSER_TEST_DATABASE_MODE === 'external-ci-service'
const reuseTestArtifacts = process.argv.includes('--reuse-test-artifacts')
const runnerArguments = [
  'scripts/test-browser.mjs',
  '--',
  '--reuse-test-artifacts',
  '--output', join(process.cwd(), 'test-results', 'lifecycle'),
  '--grep', 'MVP-013 complete product loop',
]
const lifecycleTimeout = 360_000
let activeRunner
let interruptedSignal
let cleanupFailed = false
const ownedProjects = new Set()
const activeWrapperProcesses = new Set()

process.on('SIGINT', () => requestStop('SIGINT'))
process.on('SIGTERM', () => requestStop('SIGTERM'))

try {
  if (!reuseTestArtifacts) await prepareBrowserArtifacts()
  const completedSignals = []
  for (const signal of ['SIGINT', 'SIGTERM']) {
    throwIfInterrupted()
    activeRunner = startRunner()
    const interruptedProject = await waitForReady(activeRunner, lifecycleTimeout)
    throwIfInterrupted()
    activeRunner.child.kill(signal)
    const interrupted = await waitForClose(activeRunner, 20_000)
    throwIfInterrupted()
    const expectedCode = signal === 'SIGINT' ? 130 : 143
    if (interrupted.code !== expectedCode || interrupted.signal !== null) {
      throw new Error(`${signal}-interrupted browser runner exited unexpectedly: ${JSON.stringify(interrupted)}\n${activeRunner.output}`)
    }
    activeRunner = undefined

    await assertCleanResources(interruptedProject)
    await waitForOwnedPortsClosed()

    throwIfInterrupted()
    const rerun = startRunner()
    activeRunner = rerun
    const rerunResult = await waitForClose(rerun, lifecycleTimeout)
    throwIfInterrupted()
    if (rerunResult.code !== 0 || rerunResult.signal !== null || !rerun.output.includes('1 passed')) {
      throw new Error(`${signal} cleanup rerun did not pass its focused product journey: ${JSON.stringify(rerunResult)}\n${rerun.output}`)
    }
    activeRunner = undefined

    const rerunProject = extractProjectName(rerun.output)
    if (!rerunProject) throw new Error(`could not identify the rerun Compose project after ${signal}`)
    ownedProjects.add(rerunProject)
    await assertCleanResources(rerunProject)
    await waitForOwnedPortsClosed()
    completedSignals.push(`${signal}: ${interruptedProject} removed; reran ${rerunProject}`)
  }
  process.stdout.write(`browser lifecycle passed: ${completedSignals.join('; ')}\n`)
}
catch (error) {
  process.stderr.write(`browser lifecycle failed: ${error.message}\n`)
  process.exitCode = 1
}
finally {
  if (activeRunner && isProcessOpen(activeRunner.child)) {
    try {
      await stopRunner(activeRunner)
    }
    catch {
      cleanupFailed = true
    }
  }
  for (const project of ownedProjects) {
    try {
      await cleanupComposeProject(project)
    }
    catch (error) {
      process.stderr.write(`could not verify final cleanup for ${project}: ${error.message}\n`)
      cleanupFailed = true
    }
  }
}

if (cleanupFailed) process.exitCode = 1
else if (interruptedSignal) process.exitCode = interruptedSignal === 'SIGINT' ? 130 : 143

function startRunner() {
  const child = spawn(process.execPath, runnerArguments, {
    cwd: process.cwd(),
    env: process.env,
    detached: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const runner = { child, output: '', closed: undefined }
  const appendOutput = (chunk) => {
    runner.output = `${runner.output}${chunk.toString()}`.slice(-96 * 1024)
    runner.projectName ??= extractProjectName(runner.output)
    if (runner.projectName) ownedProjects.add(runner.projectName)
  }
  child.stdout.on('data', appendOutput)
  child.stderr.on('data', appendOutput)
  runner.closed = new Promise((resolve) => {
    child.once('close', (code, signal) => resolve({ code, signal }))
  })
  return runner
}

async function waitForReady(runner, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    const project = extractProjectName(runner.output)
    if (project) {
      runner.projectName = project
      ownedProjects.add(project)
    }
    if (project && await isPortOpen(ports.broker) && await isApiReady() && await isPortOpen(ports.web)) return project
    if (runner.child.exitCode !== null || runner.child.signalCode !== null) {
      throw new Error(`browser runner exited before API/web readiness\n${runner.output}`)
    }
    await delay(100)
  }
  throw new Error(`browser runner did not reach API/web readiness within ${timeoutMilliseconds}ms\n${runner.output}`)
}

async function isApiReady() {
  try {
    const response = await fetch(`http://127.0.0.1:${ports.api}/health/ready`, { signal: AbortSignal.timeout(500) })
    const body = await response.json()
    return response.status === 200 && body?.status === 'ready'
  }
  catch {
    return false
  }
}

async function waitForClose(runner, timeoutMilliseconds) {
  let timer
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`browser runner did not exit within ${timeoutMilliseconds}ms\n${runner.output}`)), timeoutMilliseconds)
  })
  try {
    return await Promise.race([runner.closed, timeout])
  }
  finally {
    clearTimeout(timer)
  }
}

function extractProjectName(output) {
  return output.match(/pulsegrid-browser-\d+-\d+/u)?.[0]
}

async function assertCleanResources(project) {
  const [containers, networks, volumes] = await Promise.all([
    runDocker(['compose', '-p', project, '--profile', 'test', 'ps', '-aq']),
    runDocker(['network', 'ls', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.Name}}']),
    runDocker(['volume', 'ls', '--filter', `label=com.docker.compose.project=${project}`, '--format', '{{.Name}}']),
  ])
  for (const [kind, result] of [['container', containers], ['network', networks], ['volume', volumes]]) {
    if (result.code !== 0) throw new Error(`could not inspect owned Compose ${kind}s for ${project}: ${result.stderr}`)
    if (result.stdout.trim() !== '') throw new Error(`owned Compose ${kind}s remain for ${project}: ${result.stdout.trim()}`)
  }
}

async function waitForOwnedPortsClosed() {
  const ownedPorts = externalDatabaseMode
    ? [ports.broker, ports.api, ports.web]
    : [ports.database, ports.broker, ports.api, ports.web]
  const deadline = Date.now() + 15_000
  while (Date.now() < deadline) {
    const open = []
    for (const port of ownedPorts) {
      if (await isPortOpen(port)) open.push(port)
    }
    if (open.length === 0) return
    await delay(100)
  }
  const stillOpen = []
  for (const port of ownedPorts) {
    if (await isPortOpen(port)) stillOpen.push(port)
  }
  if (stillOpen.length > 0) throw new Error(`owned test ports remain open after cleanup: ${stillOpen.join(', ')}`)
}

async function isPortOpen(port) {
  return await new Promise((resolve) => {
    const socket = net.createConnection({ host: '127.0.0.1', port })
    const finish = (open) => {
      socket.destroy()
      resolve(open)
    }
    socket.once('connect', () => finish(true))
    socket.once('error', () => finish(false))
    socket.setTimeout(300, () => finish(false))
  })
}

async function prepareBrowserArtifacts() {
  const environment = { ...process.env }
  for (const key of Object.keys(environment)) {
    if (key.startsWith('PULSEGRID_') || key.startsWith('NUXT_')) delete environment[key]
  }
  environment.NUXT_TELEMETRY_DISABLED = '1'
  const apiBuild = await runOwnedCommand('node', ['scripts/build-api-test.mjs'], {
    env: environment,
    timeoutMilliseconds: 180_000,
    label: 'test API artifact build',
    inheritOutput: true,
  })
  if (apiBuild.code !== 0) throw new Error(`test API artifact build exited ${apiBuild.code}`)
  const webBuild = await runOwnedCommand('node', ['scripts/build-web.mjs', 'test'], {
    env: environment,
    timeoutMilliseconds: 180_000,
    label: 'test Nuxt artifact build',
    inheritOutput: true,
  })
  if (webBuild.code !== 0) throw new Error(`test Nuxt artifact build exited ${webBuild.code}`)
}

function requestStop(signal) {
  const nextSignal = interruptedSignal ? 'SIGKILL' : signal
  interruptedSignal ??= signal
  if (activeRunner && isProcessOpen(activeRunner.child)) signalProcessGroup(activeRunner.child, nextSignal)
  for (const signalOwnedProcess of activeWrapperProcesses) signalOwnedProcess(nextSignal)
}

function throwIfInterrupted() {
  if (interruptedSignal) throw new Error(`browser lifecycle interrupted by ${interruptedSignal}`)
}

function signalProcessGroup(child, signal) {
  if (!child.pid) return
  try {
    process.kill(-child.pid, signal)
  }
  catch (error) {
    if (error.code !== 'ESRCH') child.kill(signal)
  }
}

function isProcessOpen(child) {
  return child.exitCode === null && child.signalCode === null
}

async function stopRunner(runner) {
  signalProcessGroup(runner.child, 'SIGTERM')
  try {
    await waitForClose(runner, 5_000)
  }
  catch {
    process.stderr.write(`browser runner did not stop after SIGTERM; sending SIGKILL\n`)
    signalProcessGroup(runner.child, 'SIGKILL')
    await waitForClose(runner, 5_000)
  }
}

async function runDocker(args, { allowDuringInterruption = false } = {}) {
  const result = await runOwnedCommand('docker', args, {
    env: process.env,
    timeoutMilliseconds: 15_000,
    label: `Docker ${args[0]}`,
    allowDuringInterruption,
  })
  return result.timedOut ? { ...result, code: 1 } : result
}

async function runOwnedCommand(command, args, { env, timeoutMilliseconds, label, allowDuringInterruption = false, inheritOutput = false }) {
  if (interruptedSignal && !allowDuringInterruption) {
    throw new Error(`refusing ${label} after ${interruptedSignal}; cleanup will target owned Compose projects`)
  }
  const child = spawn(command, args, {
    cwd: process.cwd(),
    env,
    detached: true,
    stdio: inheritOutput ? 'inherit' : ['ignore', 'pipe', 'pipe'],
  })
  let pendingSignal
  const signalOwnedProcess = (signal) => {
    if (!child.pid) {
      pendingSignal = signal
      return
    }
    signalProcessGroup(child, signal)
  }
  child.once('spawn', () => {
    if (pendingSignal) {
      signalOwnedProcess(pendingSignal)
      pendingSignal = undefined
    }
  })
  activeWrapperProcesses.add(signalOwnedProcess)
  let stdout = ''
  let stderr = ''
  if (!inheritOutput) {
    child.stdout.on('data', chunk => { stdout = `${stdout}${chunk.toString()}`.slice(-16 * 1024) })
    child.stderr.on('data', chunk => { stderr = `${stderr}${chunk.toString()}`.slice(-16 * 1024) })
  }
  let timedOut = false
  let escalationTimer
  const timeoutTimer = setTimeout(() => {
    timedOut = true
    signalOwnedProcess('SIGTERM')
    escalationTimer = setTimeout(() => signalOwnedProcess('SIGKILL'), 2_000)
  }, timeoutMilliseconds)
  try {
    const result = await new Promise((resolve) => {
      let settled = false
      const finish = (value) => {
        if (settled) return
        settled = true
        resolve(value)
      }
      child.once('error', (error) => finish({ code: 1, stdout, stderr: `unable to run ${label}: ${error.message}` }))
      child.once('close', (code, signal) => finish({ code: code ?? (signal ? 1 : 0), signal, stdout, stderr }))
    })
    return timedOut
      ? { ...result, code: 1, timedOut, stderr: `${result.stderr}${label} timed out after ${timeoutMilliseconds}ms` }
      : result
  }
  finally {
    clearTimeout(timeoutTimer)
    if (escalationTimer) clearTimeout(escalationTimer)
    activeWrapperProcesses.delete(signalOwnedProcess)
  }
}

async function cleanupComposeProject(project) {
  const result = await runDocker(
    ['compose', '-p', project, '--profile', 'test', 'down', '-v', '--remove-orphans'],
    { allowDuringInterruption: true },
  )
  if (result.code !== 0) throw new Error(`Docker down exited ${result.code}: ${result.stderr}`)
}

function delay(milliseconds) {
  return new Promise(resolve => setTimeout(resolve, milliseconds))
}
