import { spawn } from 'node:child_process'
import net from 'node:net'

const ports = { database: 15432, broker: 11883, api: 18080, web: 4173 }
const externalDatabaseMode = process.env.CI === 'true'
  && process.env.PULSEGRID_BROWSER_TEST_DATABASE_MODE === 'external-ci-service'
const runnerArguments = [
  'scripts/test-browser.mjs', '--', '--grep', 'MVP-013 complete product loop',
]
const lifecycleTimeout = 360_000
let activeRunner
let ownedProject
let interruptedProject
let rerunProject

try {
  const completedSignals = []
  for (const signal of ['SIGINT', 'SIGTERM']) {
    activeRunner = startRunner()
    ownedProject = await waitForReady(activeRunner, lifecycleTimeout)
    activeRunner.child.kill(signal)
    const interrupted = await waitForClose(activeRunner, 20_000)
    const expectedCode = signal === 'SIGINT' ? 130 : 143
    if (interrupted.code !== expectedCode || interrupted.signal !== null) {
      throw new Error(`${signal}-interrupted browser runner exited unexpectedly: ${JSON.stringify(interrupted)}\n${activeRunner.output}`)
    }
    activeRunner = undefined

    interruptedProject = ownedProject
    await assertCleanResources(interruptedProject)
    await waitForOwnedPortsClosed()

    const rerun = startRunner()
    activeRunner = rerun
    const rerunResult = await waitForClose(rerun, lifecycleTimeout)
    if (rerunResult.code !== 0 || rerunResult.signal !== null || !rerun.output.includes('1 passed')) {
      throw new Error(`${signal} cleanup rerun did not pass its focused product journey: ${JSON.stringify(rerunResult)}\n${rerun.output}`)
    }
    activeRunner = undefined

    rerunProject = extractProjectName(rerun.output)
    if (!rerunProject) throw new Error(`could not identify the rerun Compose project after ${signal}`)
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
  if (activeRunner && activeRunner.child.exitCode === null && activeRunner.child.signalCode === null) {
    activeRunner.child.kill('SIGTERM')
    try {
      await waitForClose(activeRunner, 12_000)
    }
    catch {
      activeRunner.child.kill('SIGKILL')
    }
  }
  for (const project of new Set([ownedProject, interruptedProject, rerunProject, activeRunner?.projectName].filter(Boolean))) {
    try {
      await cleanupComposeProject(project)
    }
    catch (error) {
      process.stderr.write(`could not verify final cleanup for ${project}: ${error.message}\n`)
      process.exitCode = 1
    }
  }
}

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
      ownedProject = project
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

async function runDocker(args) {
  const child = spawn('docker', args, { cwd: process.cwd(), env: process.env, stdio: ['ignore', 'pipe', 'pipe'] })
  let stdout = ''
  let stderr = ''
  child.stdout.on('data', chunk => { stdout = `${stdout}${chunk.toString()}`.slice(-16 * 1024) })
  child.stderr.on('data', chunk => { stderr = `${stderr}${chunk.toString()}`.slice(-16 * 1024) })
  const result = await new Promise((resolve, reject) => {
    child.once('error', reject)
    child.once('close', (code, signal) => resolve({ code: code ?? (signal ? 1 : 0), stdout, stderr }))
  })
  return result
}

async function cleanupComposeProject(project) {
  const result = await runDocker(['compose', '-p', project, '--profile', 'test', 'down', '-v', '--remove-orphans'])
  if (result.code !== 0) process.stderr.write(`could not finish cleanup for ${project}: ${result.stderr}\n`)
}

function delay(milliseconds) {
  return new Promise(resolve => setTimeout(resolve, milliseconds))
}
