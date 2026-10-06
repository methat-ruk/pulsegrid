import { randomBytes, randomUUID } from 'node:crypto'
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import net from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const rootDirectory = process.cwd()
const brokerPort = 11883
const databasePort = 15432
const apiPort = 18080
const suites = ['core', 'deadlines', 'outage', 'shutdown']
const selectedSuite = process.argv[2] ?? 'all'
if (selectedSuite !== 'all' && !suites.includes(selectedSuite)) {
  throw new Error(`unknown MQTT integration suite ${selectedSuite}; expected all or ${suites.join(', ')}`)
}
const selectedSuites = selectedSuite === 'all' ? suites : [selectedSuite]
const projectName = `pulsegrid-mqtt-test-${process.pid}-${Date.now()}`
const password = `test-${randomBytes(18).toString('base64url')}`
const databaseUrl = `postgres://pulsegrid:${password}@127.0.0.1:${databasePort}/pulsegrid_test?sslmode=disable`
const environment = {
  ...process.env,
  PULSEGRID_ENV: 'test',
  PULSEGRID_IDENTITY_MODE: 'development',
  PULSEGRID_MQTT_INGESTION_MODE: 'development',
  PULSEGRID_MQTT_COMMAND_MODE: 'development',
  PULSEGRID_MQTT_BROKER_URL: `mqtt://127.0.0.1:${brokerPort}`,
  PULSEGRID_DATABASE_URL: databaseUrl,
  PULSEGRID_HTTP_HOST: '127.0.0.1',
  PULSEGRID_HTTP_PORT: String(apiPort),
  PULSEGRID_POSTGRES_PASSWORD: password,
}
const runningChildren = new Set()
let stopRequested = false
let stopCode = 0
let cleanupStarted = false
let apiChild = null
let apiExit = null
let apiOutput = ''
const simulators = new Map()

const simulatorDirectory = mkdtempSync(join(tmpdir(), 'pulsegrid-mqtt-'))
const apiBinary = join(simulatorDirectory, 'api')
const simulatorBinary = join(simulatorDirectory, 'device-simulator')

for (const port of [brokerPort, databasePort, apiPort]) {
  if (await isPortOpen(port)) {
    console.error(`refusing MQTT ingestion integration: 127.0.0.1:${port} is already in use`)
    process.exit(1)
  }
}

process.once('SIGINT', () => requestStop('SIGINT', 130))
process.once('SIGTERM', () => requestStop('SIGTERM', 143))

let exitCode = 1
try {
  const built = await buildBinaries()
  const started = built && await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'postgres-test', 'mqtt-test'], environment, 180_000)
  const migrated = started && await prepareDatabase()
  if (migrated && !stopRequested) {
    for (const suite of selectedSuites) {
      if (stopRequested) break
      console.log(`mqtt-suite-start suite=${suite} timestamp=${new Date().toISOString()}`)
      if (suite === 'core') await runCoreSuite()
      if (suite === 'deadlines') await runDeadlineSuite()
      if (suite === 'outage') await runOutageSuite()
      if (suite === 'shutdown') await runShutdownSuite()
      console.log(`mqtt-suite-finish suite=${suite} timestamp=${new Date().toISOString()}`)
    }
    exitCode = stopRequested ? stopCode : 0
    if (exitCode === 0) console.log(`MQTT ${selectedSuite} integration passed`)
  }
} catch (error) {
  console.error(`MQTT ingestion integration failed: ${error.message}`)
  if (apiOutput) console.error(`API output (last 12 KiB):\n${apiOutput.slice(-12 * 1024)}`)
  for (const [deviceID, simulator] of simulators) {
    if (simulator.output) console.error(`command simulator ${deviceID} output (last 8 KiB):\n${simulator.output.slice(-8 * 1024)}`)
  }
  exitCode = stopRequested ? stopCode : 1
} finally {
  if (simulators.size) await stopCommandSimulator()
  if (apiChild) await stopApi()
  const cleanupSucceeded = await cleanup()
  if (!cleanupSucceeded && !stopRequested) exitCode = 1
  if (stopRequested) exitCode = stopCode
}
process.exit(exitCode)

async function runCoreSuite() {
  await startApi()
  await waitForReady()
  const deviceID = await createDevice()
  const tenantB = await createRegisteredTenantFixture()
  await timed('telemetry-regression', async () => {
    await createThresholdRule(deviceID)
    await testMalformedTelemetryNoWrite(deviceID)
    const simulatorTelemetry = await publishSimulator(deviceID)
    const replayCase = await publishInvalidCases(deviceID)
    const duplicateMessageID = replayCase.messageID
    await assertPersistedObservation(deviceID, simulatorTelemetry.messageID, 23.5, 1)
    await assertPersistedObservation(deviceID, duplicateMessageID, 24.5, 1)
    await assertAlertForMessage(deviceID, simulatorTelemetry.messageID, 23.5, 20)
    await assertAlertForMessage(deviceID, duplicateMessageID, 24.5, 20)
    await testTelemetryReplayAndConflictAfterRestart(deviceID, replayCase.payload, duplicateMessageID)
    await testAlertPersistenceFailure(deviceID)
    const projectionTelemetry = await testPersistenceProjection(deviceID, simulatorTelemetry)
    await testRegisteredTenantMQTTIsolation(deviceID, tenantB)
    await testBrokerPayloadCap(deviceID)
    assertNoRawTelemetryInLogs()
    await testRetainedMessage(deviceID)
    await assertPersistedObservation(deviceID, projectionTelemetry.newerMessageID, 31, 1)
  })
  await timed('command-outcomes', () => testCommandOutcomes(deviceID))
  await timed('broker-recovery', () => testBrokerRecovery(deviceID))
  if (simulators.size && !await stopCommandSimulator()) throw new Error('command simulator did not stop cleanly')
  await stopApiAndAssertComplete('core suite')
}

async function runDeadlineSuite() {
  await startApi()
  await waitForReady()
  const [ackDeviceID, silentDeviceID] = await Promise.all([createDevice(), createDevice()])
  await Promise.all([
    startCommandSimulator(ackDeviceID, 'ack-only'),
    startCommandSimulator(silentDeviceID, 'silent'),
  ])
  const ackOnly = await createCommand(ackDeviceID)
  const silent = await createCommand(silentDeviceID)
  const acknowledged = await waitForCommandStatus(ackOnly.command.id, 'ACKNOWLEDGED')
  if (!acknowledged.acknowledgedAt || acknowledged.terminalAt !== null) {
    throw new Error(`ACK-only command milestones = ${JSON.stringify(acknowledged)}`)
  }
  await waitForSimulatorLogFields(silentDeviceID, [`command_id=${silent.command.id}`, 'reason_code=simulator_command_silent'], 20_000, 'silent command receipt')
  assertNoRawCommandInLogs()

  await restartApi()
  const acknowledgedAfterRestart = await queryCommand(ackOnly.command.id)
  if (acknowledgedAfterRestart.status !== 'ACKNOWLEDGED' || acknowledgedAfterRestart.acknowledgedAt !== acknowledged.acknowledgedAt) {
    throw new Error(`API restart changed acknowledged command state: ${JSON.stringify(acknowledgedAfterRestart)}`)
  }
  const silentAfterRestart = await queryCommand(silent.command.id)
  if (silentAfterRestart.status !== 'DISPATCHED' || silentAfterRestart.acknowledgedAt !== null) {
    throw new Error(`API restart changed silent command state: ${JSON.stringify(silentAfterRestart)}`)
  }

  const [ackTimedOut, silentTimedOut] = await Promise.all([
    timed('ack-only-expiry', () => waitForCommandTimeout(ackOnly.command)),
    timed('silent-expiry', () => waitForCommandTimeout(silent.command)),
  ])
  if (!ackTimedOut.acknowledgedAt || !ackTimedOut.terminalAt) {
    throw new Error(`ACK-only timeout lost acknowledgement evidence: ${JSON.stringify(ackTimedOut)}`)
  }
  if (!silentTimedOut.dispatchedAt || silentTimedOut.acknowledgedAt !== null || !silentTimedOut.terminalAt) {
    throw new Error(`silent command timeout milestones = ${JSON.stringify(silentTimedOut)}`)
  }
  if (!await stopCommandSimulator(ackDeviceID) || !await stopCommandSimulator(silentDeviceID)) {
    throw new Error('deadline command simulators did not stop cleanly')
  }
  assertNoRawCommandInLogs()
  await stopApiAndAssertComplete('deadline suite')
}

async function runOutageSuite() {
  await startApi()
  await waitForReady()
  const deviceID = await createDevice()
  await startCommandSimulator(deviceID, 'success')
  await timed('broker-outage-expiry', async () => {
    const stopped = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'stop', 'mqtt-test'], environment, 60_000)
    if (!stopped) throw new Error('unable to stop test broker for outage-expiry test')
    await waitForNotReady()
    await waitForPortClosed(brokerPort, 10_000)
    const outageCommand = await createCommand(deviceID)
    if (outageCommand.command.status !== 'PENDING') {
      throw new Error(`command created while broker is down had status ${outageCommand.command.status}, want PENDING`)
    }
    const timedOut = await waitForCommandTimeout(outageCommand.command)
    if (timedOut.status !== 'TIMED_OUT' || !timedOut.terminalAt) {
      throw new Error(`broker-outage command did not expire: ${JSON.stringify(timedOut)}`)
    }
    if (await isPortOpen(brokerPort)) throw new Error('broker became available before expiry was observed')
    const readiness = await fetch(`http://127.0.0.1:${apiPort}/health/ready`)
    if (readiness.status !== 503) throw new Error(`API unexpectedly became ready while broker remained down: ${readiness.status}`)
    if (getSimulatorOutput(deviceID).includes(`command_id=${outageCommand.command.id}`)) {
      throw new Error(`broker outage delivered command ${outageCommand.command.id} to the device`)
    }

    const simulatorOffset = getSimulatorOutput(deviceID).length
    const started = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'mqtt-test'], environment, 120_000)
    if (!started) throw new Error('unable to restart test broker after outage expiry')
    await waitForReady()
    await waitForSimulatorLogSince(deviceID, simulatorOffset, 'reason_code=simulator_command_ready', 20_000, 'simulator command resubscription after broker recovery')
    if (getSimulatorOutput(deviceID).slice(simulatorOffset).includes(`command_id=${outageCommand.command.id}`)) {
      throw new Error(`expired broker-outage command ${outageCommand.command.id} was delivered after recovery`)
    }
    const recoveryCommand = await createCommand(deviceID)
    const recoveredCommand = await waitForCommandStatus(recoveryCommand.command.id, 'COMPLETED', 20_000)
    assertCommandMilestones(recoveredCommand, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])
  })
  if (!await stopCommandSimulator(deviceID)) throw new Error('outage command simulator did not stop cleanly')
  await stopApiAndAssertComplete('broker outage suite')
}

async function runShutdownSuite() {
  return timed('response-shutdown-drain', runShutdownDrainEvidence)
}

async function runShutdownDrainEvidence() {
  await startApi({ ...environment, PULSEGRID_SHUTDOWN_TIMEOUT: '1s' })
  await waitForReady()
  const deviceID = await createDevice()
  await startCommandSimulator(deviceID, 'silent')
  const command = await createCommand(deviceID)
  await waitForSimulatorLogFields(deviceID, [`command_id=${command.command.id}`, 'reason_code=simulator_command_silent'], 20_000, 'shutdown test command delivery')
  const dispatched = await waitForCommandStatus(command.command.id, 'DISPATCHED')
  if (!dispatched.dispatchedAt) throw new Error(`shutdown test command was not dispatched: ${JSON.stringify(dispatched)}`)

  const lockHolder = await startCommandLockHolder(command.command.id)
  try {
    await publishCommandResponse(deviceID, command.command.id, 'ACK')
    await waitForAPICommandLock(10_000)
    await publishCommandResponse(deviceID, command.command.id, 'COMPLETED')
    await delay(300)

    const shutdownOffset = apiOutput.length
    const child = apiChild
    const exit = apiExit
    child.kill('SIGTERM')
    const result = await Promise.race([exit, delay(10_000).then(() => ({ status: 1, signal: 'timeout' }))])
    apiChild = null
    if (result.status !== 1 || result.signal === 'timeout') {
      child.kill('SIGKILL')
      throw new Error(`API forced-deadline shutdown exit = ${JSON.stringify(result)}, want status 1 after deadline`)
    }
    const shutdownLogs = apiOutput.slice(shutdownOffset)
    const deadlineIndex = shutdownLogs.indexOf('reason_code=command_response_drain_deadline_expired')
    const workerStoppedIndex = shutdownLogs.indexOf('reason_code=command_response_worker_stopped')
    if (deadlineIndex < 0 || !shutdownLogs.slice(deadlineIndex).includes('queued_responses=1')) {
      throw new Error(`shutdown did not record one queued response at forced deadline: ${shutdownLogs}`)
    }
    if (workerStoppedIndex < deadlineIndex) {
      throw new Error(`response worker completion did not follow forced drain deadline: ${shutdownLogs}`)
    }
    if (!shutdownLogs.includes('reason_code=shutdown_incomplete')) {
      throw new Error(`API did not report its forced shutdown deadline: ${shutdownLogs}`)
    }
    if (!await stopCommandSimulator(deviceID)) throw new Error('shutdown test simulator did not stop cleanly')
  } finally {
    await lockHolder.release()
  }

  const row = await runPSQL(`SELECT status || ':' || coalesce(acknowledged_at::text, 'null') || ':' || coalesce(terminal_at::text, 'null') FROM commands WHERE id = '${command.command.id}'::uuid;`)
  if (row.status !== 0 || row.stdout.trim() !== 'DISPATCHED:null:null') {
    throw new Error(`forced shutdown applied a response or terminalized the row: ${row.stdout}${row.stderr}`)
  }
  if (apiOutput.includes('"outcome"') || apiOutput.includes('"commandId"')) {
    throw new Error('API logs contain a raw MQTT command response payload')
  }
}

async function stopApiAndAssertComplete(label) {
  if (!await stopApi()) throw new Error(`API did not drain and stop cleanly after ${label}`)
  if (!apiOutput.includes('reason_code=shutdown_complete')) {
    throw new Error(`API shutdown completion was not logged after ${label}`)
  }
}

async function timed(name, task) {
  const startedAt = Date.now()
  console.log(`mqtt-scenario-start scenario=${name} timestamp=${new Date(startedAt).toISOString()}`)
  try {
    return await task()
  } finally {
    const finishedAt = Date.now()
    console.log(`mqtt-scenario-finish scenario=${name} timestamp=${new Date(finishedAt).toISOString()} duration_ms=${finishedAt - startedAt}`)
  }
}

async function buildBinaries() {
  const apiBuilt = await run('go', ['-C', 'apps/api', 'build', '-o', apiBinary, './cmd/api'], process.env, 180_000)
  const simulatorBuilt = apiBuilt && await run('go', ['-C', 'apps/api', 'build', '-o', simulatorBinary, './cmd/device-simulator'], process.env, 180_000)
  return apiBuilt && simulatorBuilt
}

async function prepareDatabase() {
  return await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'down'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'migrate', 'up'], environment, 120_000) &&
    await run('go', ['-C', 'apps/api', 'run', './cmd/db', 'seed'], environment, 120_000)
}

async function startApi(apiEnvironment = environment) {
  if (apiChild) throw new Error('API is already running')
  apiOutput = ''
  apiChild = spawn(apiBinary, [], {
    cwd: rootDirectory,
    env: apiEnvironment,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  runningChildren.add(apiChild)
  apiChild.stdout.on('data', (chunk) => { apiOutput += chunk.toString() })
  apiChild.stderr.on('data', (chunk) => { apiOutput += chunk.toString() })
  const child = apiChild
  apiExit = new Promise((resolve) => {
    child.once('close', (status, signal) => {
      runningChildren.delete(child)
      resolve({ status: status ?? 1, signal })
    })
    child.once('error', (error) => {
      runningChildren.delete(child)
      resolve({ status: 1, signal: null, error })
    })
  })
}

async function stopApi() {
  if (!apiChild) return true
  const child = apiChild
  const exit = apiExit
  apiChild = null
  child.kill('SIGTERM')
  const result = await Promise.race([exit, delay(15_000).then(() => ({ status: 1, signal: 'timeout' }))])
  if (result.status !== 0) {
    child.kill('SIGKILL')
    console.error(`API process did not exit cleanly: ${JSON.stringify(result)}`)
    return false
  }
  return true
}

async function waitForReady() {
  await waitForHTTP((response) => response.status === 200 && response.body?.status === 'ready', 30_000, 'API readiness')
}

async function waitForNotReady() {
  await waitForHTTP((response) => response.status === 503 && response.body?.reason === 'dependency_unavailable', 15_000, 'API dependency-unavailable readiness')
}

async function waitForHTTP(predicate, timeout, label) {
  const deadline = Date.now() + timeout
  let lastStatus = 'unreachable'
  while (Date.now() < deadline && !stopRequested) {
    try {
      const response = await fetch(`http://127.0.0.1:${apiPort}/health/ready`)
      const body = await response.json()
      lastStatus = `${response.status} ${JSON.stringify(body)}`
      if (predicate({ status: response.status, body })) return
    } catch {
      lastStatus = 'unreachable'
    }
    await delay(150)
  }
  throw new Error(`${label} was not observed before timeout; last state: ${lastStatus}`)
}

async function createDevice() {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'mutation CreateDevice($input: CreateDeviceInput!) { createDevice(input: $input) { id } }',
      variables: { input: { deviceKey: `mqtt-ingestion-${randomUUID()}`, displayName: 'MQTT ingestion integration' } },
    }),
  })
  const body = await response.json()
  const deviceID = body?.data?.createDevice?.id
  if (response.status !== 200 || typeof deviceID !== 'string') {
    throw new Error(`device registration failed: ${JSON.stringify(body)}`)
  }
  return deviceID
}

async function createRegisteredTenantFixture() {
  const organizationID = randomUUID()
  const deviceID = randomUUID()
  const tenantSlug = `mvp013-${randomUUID().replaceAll('-', '')}`
  const deviceKey = `mvp013-device-${randomUUID()}`
  const inserted = await runPSQL(`
    WITH new_organization AS (
      INSERT INTO organizations (id, slug, display_name)
      VALUES ('${organizationID}'::uuid, '${tenantSlug}', 'MVP-013 isolation fixture')
      RETURNING id
    ), new_device AS (
      INSERT INTO devices (id, organization_id, device_key, display_name)
      SELECT '${deviceID}'::uuid, id, '${deviceKey}', 'MVP-013 isolation device'
      FROM new_organization
      RETURNING id
    ) SELECT id FROM new_device;
  `)
  if (inserted.status !== 0 || inserted.stdout.trim() !== deviceID) {
    throw new Error(`could not create registered tenant isolation fixture: ${inserted.stderr}`)
  }
  return { organizationID, tenantSlug, deviceID }
}

async function createThresholdRule(deviceID) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'mutation CreateThresholdRule($input: CreateThresholdRuleInput!) { createThresholdRule(input: $input) { id enabled revision comparator thresholdCelsius } }',
      variables: { input: { deviceId: deviceID, comparator: 'GT', thresholdCelsius: 20 } },
    }),
  })
  const body = await response.json()
  const rule = body?.data?.createThresholdRule
  if (response.status !== 200 || typeof rule?.id !== 'string' || rule.enabled !== true || rule.revision !== 1 || rule.thresholdCelsius !== 20) {
    throw new Error(`threshold rule creation failed: ${JSON.stringify(body)}`)
  }
  return rule.id
}

async function createCommand(deviceID, idempotencyKey = randomUUID()) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'mutation CreateCommand($input: CreateCommandInput!) { createCommand(input: $input) { id deviceId type status createdAt expiresAt dispatchedAt acknowledgedAt terminalAt failureCode } }',
      variables: { input: { deviceId: deviceID, type: 'PING', idempotencyKey } },
    }),
  })
  const body = await response.json()
  const command = body?.data?.createCommand
  if (response.status !== 200 || body.errors || typeof command?.id !== 'string' || typeof command.status !== 'string') {
    throw new Error(`command creation failed: ${JSON.stringify(body)}`)
  }
  return { command, idempotencyKey }
}

async function queryCommand(commandID) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'query Command($id: ID!) { command(id: $id) { id deviceId type status createdAt expiresAt dispatchedAt acknowledgedAt terminalAt failureCode } }',
      variables: { id: commandID },
    }),
  })
  const body = await response.json()
  const command = body?.data?.command
  if (response.status !== 200 || body.errors || !command) {
    throw new Error(`command query failed: ${JSON.stringify(body)}`)
  }
  return command
}

async function waitForCommandStatus(commandID, expectedStatus, timeout = 20_000) {
  const deadline = Date.now() + timeout
  let command
  while (Date.now() < deadline && !stopRequested) {
    command = await queryCommand(commandID)
    if (command.status === expectedStatus) return command
    if (['COMPLETED', 'FAILED', 'TIMED_OUT'].includes(command.status)) {
      throw new Error(`command ${commandID} reached ${command.status}, expected ${expectedStatus}`)
    }
    await delay(200)
  }
  throw new Error(`command ${commandID} did not reach ${expectedStatus}; latest=${JSON.stringify(command)}`)
}

async function waitForCommandTimeout(command) {
  const expiry = Date.parse(command.expiresAt)
  const timeout = Math.max(20_000, expiry - Date.now() + 15_000)
  return waitForCommandStatus(command.id, 'TIMED_OUT', timeout)
}

async function testCommandOutcomes(deviceID) {
  assertNoRawCommandInLogs()
  await restartApi({ ...environment, PULSEGRID_MQTT_COMMAND_MODE: 'disabled' })
  const recovered = await createCommand(deviceID)
  if ((await queryCommand(recovered.command.id)).status !== 'PENDING') {
    throw new Error('disabled command mode unexpectedly dispatched persisted intent')
  }
  await restartApi(environment)
  await startCommandSimulator(deviceID, 'success')
  const recoveredCommand = await waitForCommandStatus(recovered.command.id, 'COMPLETED', 25_000)
  assertCommandMilestones(recoveredCommand, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])
  if (!await stopCommandSimulator()) throw new Error('restart recovery simulator did not stop cleanly')

  await restartApi({ ...environment, PULSEGRID_MQTT_COMMAND_MODE: 'disabled' })
  const postPublish = await createCommand(deviceID)
  const triggerSQL = `CREATE OR REPLACE FUNCTION test_reject_command_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id = '${postPublish.command.id}'::uuid AND NEW.status = 'DISPATCHED' AND OLD.status IS DISTINCT FROM NEW.status THEN RAISE EXCEPTION 'forced command dispatch write failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_reject_command_dispatch BEFORE UPDATE ON commands FOR EACH ROW EXECUTE FUNCTION test_reject_command_dispatch();`
  const dropTriggerSQL = 'DROP TRIGGER IF EXISTS test_reject_command_dispatch ON commands; DROP FUNCTION IF EXISTS test_reject_command_dispatch();'
  const trigger = await runPSQL(triggerSQL)
  if (trigger.status !== 0) throw new Error(`could not install command dispatch failure trigger: ${trigger.stderr}`)
  try {
    await restartApi(environment)
    const failureLogOffset = apiOutput.length
    await waitForLogSince(failureLogOffset, 'reason_code=command_dispatch_state_write_failed', 15_000, 'post-publish command state-write failure')
    const stillPending = await queryCommand(postPublish.command.id)
    if (stillPending.status !== 'PENDING' || stillPending.dispatchedAt !== null) {
      throw new Error(`failed dispatch state write changed command: ${JSON.stringify(stillPending)}`)
    }
    assertNoRawCommandInLogs()
    await restartApi({ ...environment, PULSEGRID_MQTT_COMMAND_MODE: 'disabled' })
    const dropped = await runPSQL(dropTriggerSQL)
    if (dropped.status !== 0) throw new Error(`could not remove command dispatch failure trigger: ${dropped.stderr}`)
    await restartApi(environment)
    await startCommandSimulator(deviceID, 'success')
    const recoveredPostPublish = await waitForCommandStatus(postPublish.command.id, 'COMPLETED', 40_000)
    assertCommandMilestones(recoveredPostPublish, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])
    if (!await stopCommandSimulator()) throw new Error('post-publish recovery simulator did not stop cleanly')
  } finally {
    const dropped = await runPSQL(dropTriggerSQL)
    if (dropped.status !== 0) throw new Error(`could not clean up command dispatch failure trigger: ${dropped.stderr}`)
  }

  const success = await createCommand(deviceID)
  await startCommandSimulator(deviceID, 'success')
  let completed = await waitForCommandStatus(success.command.id, 'COMPLETED')
  assertCommandMilestones(completed, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])

  const duplicate = await createCommand(deviceID, success.idempotencyKey)
  if (duplicate.command.id !== success.command.id) {
    throw new Error(`idempotent command retry returned a second ID: ${duplicate.command.id}`)
  }
  const rowCount = await runPSQL(`SELECT count(*) FROM commands WHERE id = '${success.command.id}'::uuid;`)
  if (rowCount.status !== 0 || rowCount.stdout.trim() !== '1') {
    throw new Error(`idempotent retry stored ${rowCount.stdout.trim()} command rows`)
  }
  if (!await stopCommandSimulator()) throw new Error('success command simulator did not stop cleanly')

  const failure = await createCommand(deviceID)
  await startCommandSimulator(deviceID, 'failure')
  const failed = await waitForCommandStatus(failure.command.id, 'FAILED')
  if (failed.failureCode !== 'DEVICE_REPORTED_FAILURE') {
    throw new Error(`device failure code = ${failed.failureCode}`)
  }
  assertCommandMilestones(failed, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])
  if (!await stopCommandSimulator()) throw new Error('failure command simulator did not stop cleanly')
  assertNoRawCommandInLogs()
}

async function restartApi(apiEnvironment = environment) {
  if (!await stopApi()) throw new Error('API did not stop cleanly before restart')
  if (!apiOutput.includes('reason_code=shutdown_complete')) {
    throw new Error('API shutdown completion was not logged before restart')
  }
  await startApi(apiEnvironment)
  await waitForReady()
}

function assertCommandMilestones(command, fields) {
  for (const field of fields) {
    if (!command[field]) throw new Error(`command ${command.id} is missing ${field}: ${JSON.stringify(command)}`)
  }
}

function assertNoRawCommandInLogs() {
  if (apiOutput.includes('schemaVersion') || apiOutput.includes('"outcome"') || apiOutput.includes('"commandId"')) {
    throw new Error('API logs contain a raw MQTT command or response payload')
  }
}

async function startCommandSimulator(deviceID, responseMode) {
  if (simulators.has(deviceID)) throw new Error(`command simulator for device ${deviceID} is already running`)
  const simulatorEnvironment = {
    ...environment,
    PULSEGRID_MQTT_DEVICE_ID: deviceID,
    PULSEGRID_MQTT_TENANT_SLUG: 'pulsegrid-dev',
    PULSEGRID_SIMULATOR_MODE: 'commands',
    PULSEGRID_SIMULATOR_COMMAND_RESPONSE: responseMode,
  }
  const child = spawn(simulatorBinary, [], {
    cwd: rootDirectory,
    env: simulatorEnvironment,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const simulator = { child, output: '', exit: null }
  simulators.set(deviceID, simulator)
  runningChildren.add(child)
  child.stdout.on('data', (chunk) => { simulator.output += chunk.toString() })
  child.stderr.on('data', (chunk) => { simulator.output += chunk.toString() })
  simulator.exit = new Promise((resolve) => {
    child.once('close', (status, signal) => {
      runningChildren.delete(child)
      resolve({ status: status ?? 1, signal })
    })
    child.once('error', (error) => {
      runningChildren.delete(child)
      resolve({ status: 1, signal: null, error })
    })
  })
  await waitForSimulatorLog(deviceID, 'reason_code=simulator_command_ready', 15_000, `command simulator ${responseMode} subscription`)
}

async function stopCommandSimulator(deviceID) {
  const selected = deviceID === undefined ? [...simulators.keys()] : [deviceID]
  let success = true
  for (const id of selected) {
    const simulator = simulators.get(id)
    if (!simulator) continue
    simulators.delete(id)
    simulator.child.kill('SIGTERM')
    const result = await Promise.race([simulator.exit, delay(15_000).then(() => ({ status: 1, signal: 'timeout' }))])
    if (result.status !== 0) {
      simulator.child.kill('SIGKILL')
      console.error(`command simulator ${id} did not exit cleanly: ${JSON.stringify(result)}`)
      success = false
    }
  }
  return success
}

function getSimulatorOutput(deviceID) {
  return simulators.get(deviceID)?.output ?? ''
}

async function waitForSimulatorLog(deviceID, text, timeout, label) {
  return waitForSimulatorLogSince(deviceID, 0, text, timeout, label)
}

async function waitForSimulatorLogSince(deviceID, offset, text, timeout, label) {
  const deadline = Date.now() + timeout
  const simulator = simulators.get(deviceID)
  if (!simulator) throw new Error(`command simulator for device ${deviceID} is not running`)
  while (Date.now() < deadline && !stopRequested) {
    if (simulator.output.slice(offset).includes(text)) return
    await delay(100)
  }
  throw new Error(`${label} was not found in command simulator output: ${text}\n${simulator.output.slice(offset)}`)
}

async function waitForSimulatorLogFields(deviceID, fields, timeout, label) {
  const deadline = Date.now() + timeout
  const simulator = simulators.get(deviceID)
  if (!simulator) throw new Error(`command simulator for device ${deviceID} is not running`)
  while (Date.now() < deadline && !stopRequested) {
    if (simulator.output.split('\n').some((line) => fields.every((field) => line.includes(field)))) return
    await delay(100)
  }
  throw new Error(`${label} was not found in command simulator output: ${fields.join(', ')}\n${simulator.output}`)
}

async function publishSimulator(deviceID) {
  const result = await runCapture(simulatorBinary, [], {
    ...environment,
    PULSEGRID_MQTT_DEVICE_ID: deviceID,
    PULSEGRID_MQTT_TENANT_SLUG: 'pulsegrid-dev',
    PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS: '23.5',
  }, 30_000)
  if (result.status !== 0) throw new Error(`simulator publish failed: ${result.stderr}`)
  const messageMatch = result.stdout.match(/message_id=([0-9a-f-]{36})/u)
  if (!messageMatch) throw new Error(`simulator output did not contain message ID: ${result.stdout}`)
  const observedAtMatch = result.stdout.match(/observed_at=([^ ]+)/u)
  if (!observedAtMatch) throw new Error(`simulator output did not contain observed time: ${result.stdout}`)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${messageMatch[1]}`], 10_000, 'valid telemetry acceptance')
  return { messageID: messageMatch[1], observedAt: observedAtMatch[1] }
}

async function publishInvalidCases(deviceID) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const validBoundaryPayload = () => JSON.stringify({ schemaVersion: 1, messageId: randomUUID(), observedAt: pastObservedAt(), temperatureCelsius: 24 })
  const cases = [
    { label: 'malformed payload', payload: '{', reason: 'telemetry_payload_malformed' },
    { label: 'application oversized payload', payload: 'x'.repeat(1100), reason: 'telemetry_payload_too_large' },
    { label: 'wrong tenant', topic: topic.replace('/tenants/pulsegrid-dev/', '/tenants/other-tenant/'), payload: validBoundaryPayload(), reason: 'telemetry_device_not_registered_for_tenant' },
    { label: 'unknown device', topic: topic.replace(deviceID, randomUUID()), payload: validBoundaryPayload(), reason: 'telemetry_device_not_registered_for_tenant' },
  ]
  for (const testCase of cases) {
    const logOffset = apiOutput.length
    const result = await publishRaw(testCase.topic ?? topic, testCase.payload, false)
    if (result.status !== 0) throw new Error(`${testCase.label} publish failed: ${result.stderr}`)
    await waitForLogSince(logOffset, `reason_code=${testCase.reason}`, 10_000, testCase.label)
  }

  const duplicateMessageID = '22222222-2222-4222-8222-222222222222'
  const duplicatePayload = JSON.stringify({ schemaVersion: 1, messageId: duplicateMessageID, observedAt: pastObservedAt(), temperatureCelsius: 24.5 })
  await publishRaw(topic, duplicatePayload, false)
  await publishRaw(topic, duplicatePayload, false)
  await waitForLogCountFields(['reason_code=telemetry_accepted', `message_id=${duplicateMessageID}`], 2, 10_000, 'duplicate logical message acceptance')
  return { messageID: duplicateMessageID, payload: duplicatePayload }
}

async function testMalformedTelemetryNoWrite(deviceID) {
  const before = await readTelemetryPersistenceSnapshot(deviceID, null)
  const logOffset = apiOutput.length
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const result = await publishRaw(topic, '{', false)
  if (result.status !== 0) throw new Error(`malformed telemetry publish failed: ${result.stderr}`)
  await waitForLogLineFieldsSince(logOffset, ['reason_code=telemetry_payload_malformed', 'payload_bytes=1'], 10_000, 'malformed telemetry rejection')
  const after = await readTelemetryPersistenceSnapshot(deviceID, null)
  if (after !== before || after !== '0:0:0:0:null:null:null') {
    throw new Error(`malformed telemetry changed persisted state: before=${before} after=${after}`)
  }
}

async function testTelemetryReplayAndConflictAfterRestart(deviceID, originalPayload, messageID) {
  const before = await readTelemetryPersistenceSnapshot(deviceID, messageID)
  if (!before.startsWith('1:1:1:1:')) throw new Error(`exact replay fixture state = ${before}, want one identity/history/state/alert row`)

  await restartApi()
  let logOffset = apiOutput.length
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const replay = await publishRaw(topic, originalPayload, false)
  if (replay.status !== 0) throw new Error(`post-restart exact replay publish failed: ${replay.stderr}`)
  await waitForLogLineFieldsSince(logOffset, ['reason_code=telemetry_accepted', `message_id=${messageID}`], 10_000, 'post-restart exact replay')
  const afterReplay = await readTelemetryPersistenceSnapshot(deviceID, messageID)
  if (afterReplay !== before) throw new Error(`exact replay changed durable state: before=${before} after=${afterReplay}`)

  const conflict = JSON.parse(originalPayload)
  conflict.temperatureCelsius += 1
  logOffset = apiOutput.length
  const conflictingPublish = await publishRaw(topic, JSON.stringify(conflict), false)
  if (conflictingPublish.status !== 0) throw new Error(`conflicting telemetry publish failed: ${conflictingPublish.stderr}`)
  await waitForLogLineFieldsSince(logOffset, ['reason_code=telemetry_message_id_conflict'], 10_000, 'conflicting message ID reuse')
  const afterConflict = await readTelemetryPersistenceSnapshot(deviceID, messageID)
  if (afterConflict !== before) throw new Error(`conflicting replay changed durable state: before=${before} after=${afterConflict}`)
}

async function readTelemetryPersistenceSnapshot(deviceID, messageID) {
  const messagePredicate = messageID === null ? '' : ` AND message_id = '${messageID}'::uuid`
  const query = `
    SELECT
      (SELECT count(*) FROM telemetry_observation_keys WHERE device_id = '${deviceID}'::uuid${messagePredicate}) || ':' ||
      (SELECT count(*) FROM telemetry_observations WHERE device_id = '${deviceID}'::uuid${messagePredicate}) || ':' ||
      (SELECT count(*) FROM device_current_state WHERE device_id = '${deviceID}'::uuid) || ':' ||
      (SELECT count(*) FROM threshold_alerts WHERE device_id = '${deviceID}'::uuid${messagePredicate}) || ':' ||
      coalesce((SELECT message_id::text FROM device_current_state WHERE device_id = '${deviceID}'::uuid), 'null') || ':' ||
      coalesce((SELECT temperature_celsius::text FROM device_current_state WHERE device_id = '${deviceID}'::uuid), 'null') || ':' ||
      coalesce((SELECT last_seen_at::text FROM device_current_state WHERE device_id = '${deviceID}'::uuid), 'null');
  `
  const result = await runPSQL(query)
  if (result.status !== 0) throw new Error(`could not inspect telemetry persistence state: ${result.stderr}`)
  return result.stdout.trim()
}

async function testRegisteredTenantMQTTIsolation(deviceIDA, tenantB) {
  const validPayload = (temperatureCelsius) => JSON.stringify({
    schemaVersion: 1,
    messageId: randomUUID(),
    observedAt: pastObservedAt(),
    temperatureCelsius,
  })
  const topicA = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceIDA}/telemetry`
  const topicB = `pulsegrid/v1/tenants/${tenantB.tenantSlug}/devices/${tenantB.deviceID}/telemetry`

  const messageB = JSON.parse(validPayload(18.25))
  let logOffset = apiOutput.length
  if ((await publishRaw(topicB, JSON.stringify(messageB), false)).status !== 0) throw new Error('registered tenant B telemetry publish failed')
  await waitForLogLineFieldsSince(logOffset, ['reason_code=telemetry_accepted', `message_id=${messageB.messageId}`, `device_id=${tenantB.deviceID}`, `organization_id=${tenantB.organizationID}`], 10_000, 'registered tenant B telemetry acceptance')
  const tenantBRows = await runPSQL(`SELECT count(*) FROM telemetry_observations WHERE device_id = '${tenantB.deviceID}'::uuid AND message_id = '${messageB.messageId}'::uuid AND temperature_celsius = 18.25;`)
  if (tenantBRows.status !== 0 || tenantBRows.stdout.trim() !== '1') throw new Error(`tenant B telemetry persistence = ${tenantBRows.stdout}${tenantBRows.stderr}`)

  for (const [label, crossedTopic] of [
    ['A slug with B device', topicA.replace(`/devices/${deviceIDA}/`, `/devices/${tenantB.deviceID}/`)],
    ['B slug with A device', topicB.replace(`/devices/${tenantB.deviceID}/`, `/devices/${deviceIDA}/`)],
  ]) {
    const crossed = JSON.parse(validPayload(19.25))
    const offset = apiOutput.length
    if ((await publishRaw(crossedTopic, JSON.stringify(crossed), false)).status !== 0) throw new Error(`${label} publish failed`)
    await waitForLogLineFieldsSince(offset, ['reason_code=telemetry_device_not_registered_for_tenant'], 10_000, label)
    const rowCount = await runPSQL(`SELECT count(*) FROM telemetry_observation_keys WHERE message_id = '${crossed.messageId}'::uuid;`)
    if (rowCount.status !== 0 || rowCount.stdout.trim() !== '0') throw new Error(`${label} wrote telemetry: ${rowCount.stdout}${rowCount.stderr}`)
  }
}

async function testPersistenceProjection(deviceID, simulatorTelemetry) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const simulatorObservedAt = new Date(simulatorTelemetry.observedAt)
  const newerMessageID = randomUUID()
  const lateMessageID = randomUUID()
  const newerObservedAt = new Date(simulatorObservedAt.getTime() + 1000).toISOString()
  const lateObservedAt = new Date(simulatorObservedAt.getTime() - 1000).toISOString()
  await publishRaw(topic, JSON.stringify({ schemaVersion: 1, messageId: newerMessageID, observedAt: newerObservedAt, temperatureCelsius: 31 }), false)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${newerMessageID}`], 10_000, 'newer telemetry acceptance')
  await publishRaw(topic, JSON.stringify({ schemaVersion: 1, messageId: lateMessageID, observedAt: lateObservedAt, temperatureCelsius: 18 }), false)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${lateMessageID}`], 10_000, 'late telemetry acceptance')
  const data = await queryTelemetry(deviceID)
  if (data.deviceCurrentState?.messageId !== newerMessageID || data.deviceCurrentState?.temperatureCelsius !== 31) {
    throw new Error(`late telemetry replaced newer current state: ${JSON.stringify(data.deviceCurrentState)}`)
  }
  await assertPersistedObservation(deviceID, newerMessageID, 31, 1)
  await assertPersistedObservation(deviceID, lateMessageID, 18, 1)
  return { newerMessageID }
}

async function queryTelemetry(deviceID) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: `query Telemetry($deviceID: ID!) { deviceCurrentState(deviceId: $deviceID) { messageId observedAt receivedAt temperatureCelsius lastSeenAt } deviceTelemetry(deviceId: $deviceID, first: 100) { edges { node { messageId observedAt receivedAt temperatureCelsius } } pageInfo { hasNextPage } } }`,
      variables: { deviceID },
    }),
  })
  const body = await response.json()
  if (response.status !== 200 || body.errors || !body.data) {
    throw new Error(`telemetry GraphQL query failed: ${JSON.stringify(body)}`)
  }
  return body.data
}

async function queryAlerts(deviceID) {
  const response = await fetch(`http://127.0.0.1:${apiPort}/graphql`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      query: 'query Alerts($deviceID: ID!) { alerts(first: 20, deviceId: $deviceID) { edges { node { id ruleId deviceId messageId observedAt receivedAt temperatureCelsius metric comparator thresholdCelsius createdAt } } pageInfo { hasNextPage } } }',
      variables: { deviceID },
    }),
  })
  const body = await response.json()
  if (response.status !== 200 || body.errors || !body.data) {
    throw new Error(`alert GraphQL query failed: ${JSON.stringify(body)}`)
  }
  return body.data.alerts.edges.map((edge) => edge.node)
}

async function assertAlertForMessage(deviceID, messageID, temperature, threshold) {
  const matches = (await queryAlerts(deviceID)).filter((alert) => alert.messageId === messageID)
  if (matches.length !== 1) throw new Error(`alert count for ${messageID} = ${matches.length}, want 1`)
  const alert = matches[0]
  if (alert.temperatureCelsius !== temperature || alert.thresholdCelsius !== threshold || alert.comparator !== 'GT' || alert.metric !== 'TEMPERATURE_CELSIUS') {
    throw new Error(`alert snapshot for ${messageID} = ${JSON.stringify(alert)}`)
  }
}

async function testAlertPersistenceFailure(deviceID) {
  const messageID = '77777777-7777-4777-8777-777777777777'
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const triggerSQL = `CREATE OR REPLACE FUNCTION test_reject_threshold_alert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.message_id = '${messageID}'::uuid THEN RAISE EXCEPTION 'forced alert persistence failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_reject_threshold_alert BEFORE INSERT ON threshold_alerts FOR EACH ROW EXECUTE FUNCTION test_reject_threshold_alert();`
  const dropSQL = 'DROP TRIGGER IF EXISTS test_reject_threshold_alert ON threshold_alerts; DROP FUNCTION IF EXISTS test_reject_threshold_alert();'
  const created = await runPSQL(triggerSQL)
  if (created.status !== 0) throw new Error(`could not install alert failure trigger: ${created.stderr}`)
  try {
    const logOffset = apiOutput.length
    const payload = JSON.stringify({ schemaVersion: 1, messageId: messageID, observedAt: pastObservedAt(), temperatureCelsius: 27 })
    const published = await publishRaw(topic, payload, false)
    if (published.status !== 0) throw new Error(`failed to publish alert failure case: ${published.stderr}`)
    await waitForLogSince(logOffset, 'reason_code=telemetry_alert_persistence_failed', 10_000, 'alert persistence failure')
    const failureLines = apiOutput.slice(logOffset).split('\n')
    if (!failureLines.some((line) => line.includes('reason_code=telemetry_alert_persistence_failed') && line.includes('ingestion_id='))) {
      throw new Error(`alert failure did not identify its message safely: ${apiOutput.slice(logOffset)}`)
    }
    if (failureLines.some((line) => line.includes('reason_code=telemetry_accepted'))) {
      throw new Error('failed alert input was logged as accepted')
    }
    const counts = await runPSQL(`SELECT (SELECT count(*) FROM telemetry_observation_keys WHERE device_id = '${deviceID}' AND message_id = '${messageID}') || ':' || (SELECT count(*) FROM telemetry_observations WHERE device_id = '${deviceID}' AND message_id = '${messageID}') || ':' || (SELECT count(*) FROM threshold_alerts WHERE device_id = '${deviceID}' AND message_id = '${messageID}');`)
    if (counts.status !== 0 || counts.stdout.trim() !== '0:0:0') {
      throw new Error(`failed alert transaction left partial rows: ${counts.stdout}${counts.stderr}`)
    }
  } finally {
    const dropped = await runPSQL(dropSQL)
    if (dropped.status !== 0) throw new Error(`could not remove alert failure trigger: ${dropped.stderr}`)
  }
  const retry = await publishRaw(topic, JSON.stringify({ schemaVersion: 1, messageId: messageID, observedAt: pastObservedAt(), temperatureCelsius: 27 }), false)
  if (retry.status !== 0) throw new Error(`failed to republish repaired alert input: ${retry.stderr}`)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${messageID}`], 10_000, 're-published alert acceptance')
  await assertAlertForMessage(deviceID, messageID, 27, 20)
}

async function assertPersistedObservation(deviceID, messageID, temperature, expectedCount) {
  const data = await queryTelemetry(deviceID)
  const points = data.deviceTelemetry?.edges?.map((edge) => edge.node).filter((point) => point.messageId === messageID) ?? []
  if (points.length !== expectedCount || points.some((point) => point.temperatureCelsius !== temperature)) {
    throw new Error(`persisted observation ${messageID} = ${JSON.stringify(points)}, want count=${expectedCount} temperature=${temperature}`)
  }
}

async function testRetainedMessage(deviceID) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  if (!await stopApi()) throw new Error('API did not stop before retained-message test')
  const retainedPayload = JSON.stringify({ schemaVersion: 1, messageId: '33333333-3333-4333-8333-333333333333', observedAt: pastObservedAt(), temperatureCelsius: 25 })
  if ((await publishRaw(topic, retainedPayload, true)).status !== 0) throw new Error('retained publish failed')
  await startApi()
  await waitForReady()
  await waitForLog('reason_code=telemetry_retained_rejected', 10_000, 'retained message rejection')
  await publishRaw(topic, '', true)
}

async function testBrokerRecovery(deviceID) {
  await startCommandSimulator(deviceID, 'success')
  const simulatorOutputOffset = getSimulatorOutput(deviceID).length
  const stopped = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'stop', 'mqtt-test'], environment, 60_000)
  if (!stopped) throw new Error('unable to stop test broker for recovery test')
  await waitForNotReady()
  const started = await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'up', '-d', '--wait', 'mqtt-test'], environment, 120_000)
  if (!started) throw new Error('unable to restart test broker for recovery test')
  await waitForReady()
  await waitForSimulatorLogSince(deviceID, simulatorOutputOffset, 'reason_code=simulator_command_ready', 20_000, 'simulator command resubscription after broker recovery')
  const recoveryCommand = await createCommand(deviceID)
  const recoveredCommand = await waitForCommandStatus(recoveryCommand.command.id, 'COMPLETED', 20_000)
  assertCommandMilestones(recoveredCommand, ['dispatchedAt', 'acknowledgedAt', 'terminalAt'])
  const messageID = randomUUID()
  await publishRaw(`pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`, JSON.stringify({ schemaVersion: 1, messageId: messageID, observedAt: pastObservedAt(), temperatureCelsius: 26 }), false)
  await waitForLogFields(['reason_code=telemetry_accepted', `message_id=${messageID}`], 10_000, 'post-recovery telemetry acceptance')
}

async function testBrokerPayloadCap(deviceID) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/telemetry`
  const subscriber = runCapture('docker', [
    'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', 'mqtt-test',
    'mosquitto_sub', '-h', '127.0.0.1', '-p', '1883', '-i', `pulsegrid-sub-${randomUUID()}`,
    '-q', '1', '-t', topic, '-C', '1', '-W', '2',
  ], environment, 5_000)
  await delay(300)
  await publishRaw(topic, 'x'.repeat(17 * 1024), false)
  const received = await subscriber
  if (received.stdout.trim() !== '') {
    throw new Error('Mosquitto forwarded a payload larger than its 16 KiB message cap')
  }
}

function assertNoRawTelemetryInLogs() {
  if (apiOutput.includes('23.5') || apiOutput.includes('schemaVersion') || apiOutput.includes('temperatureCelsius')) {
    throw new Error('API logs contain raw telemetry content')
  }
}

async function publishRaw(topic, payload, retained) {
  const args = [
    'docker', 'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', 'mqtt-test',
    'mosquitto_pub', '-h', '127.0.0.1', '-p', '1883', '-i', `pulsegrid-pub-${randomUUID()}`,
    '-q', '1', '-t', topic, '-m', payload,
  ]
  if (retained) args.push('-r')
  return runCapture(args[0], args.slice(1), environment, 15_000)
}

async function runPSQL(statement) {
  return runCapture('docker', [
    'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', '-e', `PGPASSWORD=${environment.PULSEGRID_POSTGRES_PASSWORD}`,
    'postgres-test', 'psql', '-U', 'pulsegrid', '-d', 'pulsegrid_test', '-v', 'ON_ERROR_STOP=1', '-Atc', statement,
  ], environment, 15_000)
}

async function startCommandLockHolder(commandID) {
  const child = spawn('docker', [
    'compose', '-p', projectName, '--profile', 'test', 'exec', '-T', '-e', `PGPASSWORD=${environment.PULSEGRID_POSTGRES_PASSWORD}`,
    'postgres-test', 'psql', '-U', 'pulsegrid', '-d', 'pulsegrid_test', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
  ], { cwd: rootDirectory, env: environment, stdio: ['pipe', 'pipe', 'pipe'] })
  runningChildren.add(child)
  const lockHolder = { child, output: '' }
  lockHolder.release = () => releaseCommandLockHolder(lockHolder)
  child.stdout.on('data', (chunk) => { lockHolder.output += chunk.toString() })
  child.stderr.on('data', (chunk) => { lockHolder.output += chunk.toString() })
  lockHolder.exit = new Promise((resolve) => {
    child.once('close', (status, signal) => {
      runningChildren.delete(child)
      resolve({ status: status ?? 1, signal })
    })
    child.once('error', (error) => {
      runningChildren.delete(child)
      resolve({ status: 1, signal: null, error })
    })
  })
  child.stdin.write(`BEGIN;\nSELECT id FROM commands WHERE id = '${commandID}'::uuid FOR UPDATE;\n\\echo __command_lock_acquired__\n`)
  const deadline = Date.now() + 10_000
  while (Date.now() < deadline && !stopRequested) {
    if (lockHolder.output.includes('__command_lock_acquired__')) return lockHolder
    const exit = await Promise.race([lockHolder.exit, delay(50).then(() => null)])
    if (exit) throw new Error(`command row lock holder exited early: ${JSON.stringify(exit)} ${lockHolder.output}`)
  }
  await lockHolder.release()
  throw new Error(`command row lock was not acquired: ${lockHolder.output}`)
}

async function releaseCommandLockHolder(lockHolder) {
  if (lockHolder.child.exitCode !== null || lockHolder.child.stdin.destroyed) return
  lockHolder.child.stdin.end('ROLLBACK;\n\\q\n')
  const result = await Promise.race([lockHolder.exit, delay(5_000).then(() => ({ status: 1, signal: 'timeout' }))])
  if (result.status !== 0) {
    lockHolder.child.kill('SIGTERM')
    throw new Error(`command row lock holder did not exit cleanly: ${JSON.stringify(result)} ${lockHolder.output}`)
  }
}

async function waitForAPICommandLock(timeout) {
  const deadline = Date.now() + timeout
  let last = 'unobserved'
  while (Date.now() < deadline && !stopRequested) {
    const result = await runPSQL("SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND lower(query) LIKE '%commands%' AND lower(query) LIKE '%for update%';")
    last = result.stdout.trim()
    if (result.status === 0 && Number(last) > 0) return
    await delay(150)
  }
  throw new Error(`API did not wait on the locked command row; lock-waiting query count=${last}`)
}

async function publishCommandResponse(deviceID, commandID, outcome) {
  const topic = `pulsegrid/v1/tenants/pulsegrid-dev/devices/${deviceID}/command-responses`
  const payload = JSON.stringify({ schemaVersion: 1, commandId: commandID, outcome })
  const result = await publishRaw(topic, payload, false)
  if (result.status !== 0) throw new Error(`could not publish ${outcome} command response: ${result.stderr}`)
}

async function waitForPortClosed(port, timeout) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (!await isPortOpen(port)) return
    await delay(100)
  }
  throw new Error(`127.0.0.1:${port} remained open past ${timeout}ms`)
}

async function waitForLog(text, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (apiOutput.includes(text)) return
    await delay(100)
  }
  throw new Error(`${label} was not found in API output: ${text}\nAPI output:\n${apiOutput}`)
}

async function waitForLogSince(offset, text, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (apiOutput.slice(offset).includes(text)) return
    await delay(100)
  }
  throw new Error(`${label} was not found in new API output: ${text}\nAPI output:\n${apiOutput.slice(offset)}`)
}

async function waitForLogLineFieldsSince(offset, fields, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    const lines = apiOutput.slice(offset).split('\n')
    if (lines.some(line => fields.every(field => line.includes(field)))) return
    await delay(100)
  }
  throw new Error(`${label} did not produce one log line with ${fields.join(', ')}\nAPI output:\n${apiOutput.slice(offset)}`)
}

async function waitForLogFields(fields, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (fields.every((field) => apiOutput.includes(field))) return
    await delay(100)
  }
  throw new Error(`${label} was not found in API output: ${fields.join(', ')}\nAPI output:\n${apiOutput}`)
}

async function waitForLogCountFields(fields, expected, timeout, label) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline && !stopRequested) {
    if (countLogFields(fields) >= expected) return
    await delay(100)
  }
  throw new Error(`${label} did not reach ${expected} occurrences: ${fields.join(', ')}\nAPI output:\n${apiOutput}`)
}

function countLogFields(fields) {
  return apiOutput.split('\n').filter((line) => fields.every((field) => line.includes(field))).length
}

async function cleanup() {
  if (cleanupStarted) return true
  cleanupStarted = true
  let success = true
  if (!await run('docker', ['compose', '-p', projectName, '--profile', 'test', 'down', '-v', '--remove-orphans'], environment, 120_000)) success = false
  try {
    rmSync(simulatorDirectory, { recursive: true, force: true })
  } catch (error) {
    console.error(`unable to remove temporary binaries: ${error.message}`)
    success = false
  }
  return success
}

function run(command, args, env, timeout) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { cwd: rootDirectory, env, stdio: 'inherit' })
    runningChildren.add(child)
    let settled = false
    let timedOut = false
    const timeoutID = setTimeout(() => {
      timedOut = true
      child.kill('SIGTERM')
      setTimeout(() => child.kill('SIGKILL'), 5_000).unref()
    }, timeout)
    const finish = (success) => {
      if (settled) return
      settled = true
      clearTimeout(timeoutID)
      runningChildren.delete(child)
      resolve(success && !timedOut)
    }
    child.once('error', (error) => {
      console.error(`unable to run ${command}: ${error.message}`)
      finish(false)
    })
    child.once('close', (status) => finish(status === 0))
  })
}

function runCapture(command, args, env, timeout) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { cwd: rootDirectory, env, stdio: ['ignore', 'pipe', 'pipe'] })
    runningChildren.add(child)
    let stdout = ''
    let stderr = ''
    let timedOut = false
    let settled = false
    const timeoutID = setTimeout(() => {
      timedOut = true
      child.kill('SIGTERM')
      setTimeout(() => child.kill('SIGKILL'), 5_000).unref()
    }, timeout)
    child.stdout.on('data', (chunk) => { stdout += chunk.toString() })
    child.stderr.on('data', (chunk) => { stderr += chunk.toString() })
    const finish = (status, signal, errorMessage = '') => {
      if (settled) return
      settled = true
      clearTimeout(timeoutID)
      runningChildren.delete(child)
      resolve({ status: timedOut ? 1 : (status ?? 1), signal, stdout, stderr: `${stderr}${errorMessage}` })
    }
    child.once('close', finish)
    child.once('error', (error) => finish(1, null, error.message))
  })
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

function pastObservedAt() {
  return new Date(Date.now() - 60_000).toISOString()
}

function isPortOpen(port) {
  return new Promise((resolve) => {
    const socket = net.createConnection({ host: '127.0.0.1', port })
    const finish = (open) => {
      socket.destroy()
      resolve(open)
    }
    socket.once('connect', () => finish(true))
    socket.once('error', () => finish(false))
    socket.setTimeout(500, () => finish(false))
  })
}

function requestStop(signal, code) {
  if (stopRequested) return
  stopRequested = true
  stopCode = code
  for (const child of runningChildren) child.kill(signal)
}
