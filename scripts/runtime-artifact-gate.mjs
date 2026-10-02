import { createHash } from 'node:crypto'
import { spawn, spawnSync } from 'node:child_process'
import { createServer } from 'node:net'
import { lstat, readFile, readdir, readlink, stat, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { collectRuntimeBundleProvenance } from './runtime-bundle-provenance.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

export function stable(value) {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${stable(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

export function sha256(value) {
  return createHash('sha256').update(value).digest('hex')
}

function isExactVersion(value) {
  return typeof value === 'string' && /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(value)
}

async function listTree(directory, prefix = '') {
  const entries = []
  for (const name of (await readdir(directory)).sort()) {
    const absolute = path.join(directory, name)
    const relative = prefix ? `${prefix}/${name}` : name
    const info = await lstat(absolute)
    if (info.isSymbolicLink()) entries.push({ path: relative, type: 'symlink', value: await readlink(absolute) })
    else if (info.isDirectory()) entries.push(...await listTree(absolute, relative))
    else if (info.isFile()) entries.push({ path: relative, type: 'file', value: await readFile(absolute) })
    else throw new Error(`unsupported filesystem entry: ${relative}`)
  }
  return entries
}

export async function treeDigest(directory) {
  const hash = createHash('sha256')
  const entries = await listTree(directory)
  for (const entry of entries) hash.update(`${entry.type}\0${entry.path}\0${sha256(entry.value)}\n`)
  return { digest: hash.digest('hex'), fileCount: entries.length }
}

function packageManifestMetadata(manifest) {
  return {
    name: manifest.name,
    version: manifest.version,
    dependencies: manifest.dependencies ?? {},
    optionalDependencies: manifest.optionalDependencies ?? {},
    peerDependencies: manifest.peerDependencies ?? {},
    peerDependenciesMeta: manifest.peerDependenciesMeta ?? {},
  }
}

async function physicalPackageInventory(outputDirectory) {
  const serverDirectory = path.join(outputDirectory, 'server')
  const modulesDirectory = path.join(serverDirectory, 'node_modules')
  const serverManifest = JSON.parse(await readFile(path.join(serverDirectory, 'package.json'), 'utf8'))
  if (serverManifest.packageManager) throw new Error('artifact changed the standalone Node runtime contract by declaring a package manager')
  for (const lockfile of ['pnpm-lock.yaml', 'package-lock.json', 'npm-shrinkwrap.json', 'yarn.lock']) {
    try {
      await stat(path.join(serverDirectory, lockfile))
      throw new Error(`artifact changed the standalone Node runtime contract by adding ${lockfile}`)
    } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
  }
  const dependencies = serverManifest.dependencies
  if (!dependencies || typeof dependencies !== 'object' || Array.isArray(dependencies)) {
    throw new Error('artifact server/package.json has no dependency map')
  }
  const expectedNames = Object.keys(dependencies).sort()
  const actualNames = []
  for (const entry of await readdir(modulesDirectory, { withFileTypes: true })) {
    if (entry.name.startsWith('@') && entry.isDirectory()) {
      for (const scoped of await readdir(path.join(modulesDirectory, entry.name), { withFileTypes: true })) {
        if (scoped.isDirectory() || scoped.isSymbolicLink()) actualNames.push(`${entry.name}/${scoped.name}`)
      }
    } else if (entry.isDirectory() || entry.isSymbolicLink()) actualNames.push(entry.name)
  }
  actualNames.sort()
  const packages = []
  for (const name of actualNames) {
    const packageDirectory = path.join(modulesDirectory, ...name.split('/'))
    const manifest = JSON.parse(await readFile(path.join(packageDirectory, 'package.json'), 'utf8'))
    packages.push({
      name,
      version: manifest.version,
      requestedVersion: dependencies[name],
      manifestSha256: sha256(stable(packageManifestMetadata(manifest))),
      tree: await treeDigest(packageDirectory),
    })
  }
  return { serverManifest, expectedNames, actualNames, packages }
}

export async function verifyRuntimeEvidence({
  outputDirectory,
  evidence,
  lockPath,
  expectedCommit,
  expectedRunId,
  expectedRunAttempt,
  downloadOutcome = process.env.RUNTIME_ARTIFACT_DOWNLOAD_OUTCOME,
}) {
  const failures = []
  const add = (message) => failures.push(message)
  const evidencePackages = evidence?.artifact?.packages
  const evidenceBundleProvenance = evidence?.artifact?.bundleProvenance
  if (!evidence || evidence.schemaVersion !== 2) return { failures: ['runtime evidence is missing or has an unsupported schema'] }
  if (!Array.isArray(evidencePackages) || evidencePackages.length === 0) add('runtime package evidence is missing')
  if (evidence.source?.commit !== expectedCommit && expectedCommit) add('artifact source revision does not match this workflow revision')
  if (evidence.source?.runId !== expectedRunId && expectedRunId) add('artifact was not produced by this workflow run')
  if (evidence.source?.runAttempt !== expectedRunAttempt && expectedRunAttempt) add('artifact was not produced by this workflow run attempt')
  if (evidence.source?.buildCommand !== 'corepack pnpm run web:build') add('build command provenance is missing or unexpected')
  if (evidence.source?.productionImporter !== 'apps/web-console') add('frozen production importer provenance is missing or unexpected')
  if (evidence.source?.lockfileVersion !== '9.0') add('unsupported frozen pnpm lockfile provenance')
  if (evidence.artifact?.path !== 'apps/web-console/.output') add('artifact path does not match the Nuxt production output')
  if (downloadOutcome && downloadOutcome !== 'success') {
    add('same-run production artifact download failed')
  }
  if (!/^\d+\.\d+\.\d+$/.test(evidence.source?.nodeVersion ?? '')) add('build Node version is missing')
  if (!/^\d+\.\d+\.\d+$/.test(evidence.source?.pnpmVersion ?? '')) add('build pnpm version is missing')

  let lockText
  try {
    const projectManifest = JSON.parse(await readFile(path.join(root, 'package.json'), 'utf8'))
    const projectNodeVersion = (await readFile(path.join(root, '.node-version'), 'utf8')).trim()
    if (evidence.source?.nodeVersion !== projectNodeVersion) add('build Node version does not match this revision')
    if (projectManifest.packageManager !== `pnpm@${evidence.source?.pnpmVersion}`) add('build pnpm version does not match this revision')
    lockText = await readFile(lockPath, 'utf8')
    if (sha256(lockText) !== evidence.source?.lockSha256) add('frozen root lock hash does not match build evidence')
  } catch {
    add('frozen root lock is unavailable')
  }

  let actualTree
  let inventory
  let bundleProvenance
  try {
    actualTree = await treeDigest(outputDirectory)
    if (actualTree.digest !== evidence.artifact?.sha256) add('downloaded .output hash does not match the build artifact hash')
    if (actualTree.fileCount !== evidence.artifact?.fileCount) add('downloaded .output file inventory differs from build evidence')
    inventory = await physicalPackageInventory(outputDirectory)
    bundleProvenance = await collectRuntimeBundleProvenance(outputDirectory)
  } catch (error) {
    add(`artifact or metadata cannot be inspected: ${error.message}`)
  }

  if (inventory) {
    const actualManifestHash = sha256(await readFile(path.join(outputDirectory, 'server/package.json')))
    if (actualManifestHash !== evidence.artifact?.runtimeManifestSha256) add('Nitro server/package.json hash differs from build evidence')
    if (stable(inventory.expectedNames) !== stable(inventory.actualNames)) add('Nitro dependency manifest and physical package inventory differ')
    const expectedPackages = [...(evidencePackages ?? [])].map(pkg => `${pkg.name}@${pkg.version}`).sort()
    const actualPackages = inventory.packages.map(pkg => `${pkg.name}@${pkg.version}`).sort()
    if (stable(expectedPackages) !== stable(actualPackages)) add('downloaded package/version inventory differs from build evidence')
    for (const pkg of inventory.packages) {
      if (pkg.requestedVersion !== pkg.version || !isExactVersion(pkg.requestedVersion)) add(`artifact dependency is not an exact package version: ${pkg.name}@${pkg.requestedVersion}`)
      const expected = evidencePackages?.find(item => item.name === pkg.name && item.version === pkg.version)
      if (!expected) continue
      if (pkg.manifestSha256 !== expected.packageManifestSha256) add(`package dependency/peer metadata changed after build: ${pkg.name}@${pkg.version}`)
      if (pkg.tree.digest !== expected.artifactTree?.digest || pkg.tree.fileCount !== expected.artifactTree?.fileCount) {
        add(`physical package tree changed after build: ${pkg.name}@${pkg.version}`)
      }
      if (!/^sha512-[A-Za-z0-9+/]+=*$/.test(expected.lock?.integrity ?? '')) add(`frozen lock integrity evidence is missing for ${pkg.name}@${pkg.version}`)
      if (!Array.isArray(expected.lock?.snapshotKeys) || expected.lock.snapshotKeys.length === 0) add(`frozen lock snapshot evidence is missing for ${pkg.name}@${pkg.version}`)
      if (!expected.sourceVirtualStoreKey?.startsWith(`${pkg.name.replaceAll('/', '+')}@`)) add(`frozen package resolution context is missing for ${pkg.name}@${pkg.version}`)
      if (!expected.sourceNonManifestFileProof?.nonManifestFileCount || !/^([a-f0-9]{64})$/.test(expected.sourceNonManifestFileProof?.nonManifestDigest ?? '')) {
        add(`frozen package content comparison is incomplete for ${pkg.name}@${pkg.version}`)
      }
    }
  }

  if (!bundleProvenance) add('bundled package provenance could not be read from the production artifact')
  else if (!evidenceBundleProvenance || typeof evidenceBundleProvenance !== 'object') {
    add('bundled package evidence is missing')
  }
  else {
    const comparableEvidence = {
      format: evidenceBundleProvenance.format,
      moduleCount: evidenceBundleProvenance.moduleCount,
      chunkCount: evidenceBundleProvenance.chunkCount,
      sourceMapCount: evidenceBundleProvenance.sourceMapCount,
      chunks: evidenceBundleProvenance.chunks,
      packages: (evidenceBundleProvenance.packages ?? []).map(pkg => ({
        name: pkg.name,
        version: pkg.version,
        sourceVirtualStoreKey: pkg.sourceVirtualStoreKey,
        modules: pkg.modules,
      })),
    }
    if (stable(comparableEvidence) !== stable(bundleProvenance)) {
      add('downloaded bundle source maps differ from frozen package provenance evidence')
    }
    if (evidenceBundleProvenance.format !== 'nitro-rollup-and-sourcemap-v1'
      || !Number.isInteger(evidenceBundleProvenance.moduleCount)
      || evidenceBundleProvenance.moduleCount < 1
      || evidenceBundleProvenance.chunkCount !== bundleProvenance.chunkCount
      || evidenceBundleProvenance.sourceMapCount !== bundleProvenance.sourceMapCount
      || !Array.isArray(evidenceBundleProvenance.chunks)
      || !Array.isArray(evidenceBundleProvenance.packages)) {
      add('Nitro Rollup module inventory is incomplete')
    }
    for (const pkg of evidenceBundleProvenance.packages ?? []) {
      if (typeof pkg.name !== 'string' || !isExactVersion(pkg.version) || !Array.isArray(pkg.modules) || pkg.modules.length === 0) {
        add('bundled package identity or module evidence is incomplete')
        continue
      }
      const encodedName = pkg.name.replaceAll('/', '+')
      if (pkg.sourceVirtualStoreKey !== `${encodedName}@${pkg.version}`
        && !pkg.sourceVirtualStoreKey?.startsWith(`${encodedName}@${pkg.version}_`)) {
        add(`bundled package does not identify an exact pnpm resolution: ${pkg.name}@${pkg.version}`)
      }
      if (pkg.lockVerified !== true
        || !/^sha512-[A-Za-z0-9+/]+=*$/.test(pkg.lock?.integrity ?? '')
        || !Array.isArray(pkg.lock?.packageRecords) || pkg.lock.packageRecords.length === 0
        || !Array.isArray(pkg.lock?.snapshotKeys) || pkg.lock.snapshotKeys.length === 0) {
        add(`bundled package is not tied to frozen lock integrity and snapshot evidence: ${pkg.name}@${pkg.version}`)
      }
      if (!/^[a-f0-9]{64}$/.test(pkg.packageManifestSha256 ?? '')) {
        add(`bundled package manifest evidence is missing: ${pkg.name}@${pkg.version}`)
      }
      const moduleKeys = (pkg.modules ?? []).map(module => `${module.chunk}\0${module.sourceFile}\0${module.origin}`).sort()
      const sourceProofKeys = (pkg.sourceFileProofs ?? []).map(module => `${module.chunk}\0${module.sourceFile}\0${module.origin}`).sort()
      if (stable(moduleKeys) !== stable(sourceProofKeys)
        || (pkg.sourceFileProofs ?? []).some(module => !/^[a-f0-9]{64}$/.test(module.sha256 ?? ''))) {
        add(`bundled package source file hashes do not cover its emitted module sources: ${pkg.name}@${pkg.version}`)
      }
    }
  }

  const graph = evidence.artifact?.graph
  if (!graph || graph.entry !== 'apps/web-console/.output/server/index.mjs') add('runtime import graph evidence is missing or has an unexpected entry')
  else {
    if (!Number.isInteger(graph.moduleCount) || graph.moduleCount < 1) add('runtime import graph contains no traversed modules')
    if (!Number.isInteger(graph.sourceGraphNodeCount) || graph.sourceGraphNodeCount < (evidencePackages?.length ?? 0)) add('frozen production graph node evidence is incomplete')
    if (!Number.isInteger(graph.sourceGraphEdgeCount) || graph.sourceGraphEdgeCount < 1) add('frozen production graph edge evidence is incomplete')
    if (!Array.isArray(graph.packageEdges) || graph.packageEdges.length < 1) add('runtime package-to-package edge evidence is missing')
    if (graph.unresolved?.length !== 0) add('runtime import graph has unresolved dependencies')
    if (graph.nonLiteral?.length !== 0) add('runtime import graph has imports that cannot be statically verified')
    if (graph.allEdgesInFrozenProductionGraph !== true) add('runtime import edges are not proven against the frozen production graph')
    const packageIds = new Set((evidencePackages ?? []).map(pkg => `${pkg.name}@${pkg.version}`))
    for (const edge of graph.packageEdges ?? []) {
      if (!edge.lockVerified || !edge.from || !edge.to || !edge.specifier) add('runtime graph contains an edge without frozen graph proof')
      if (!packageIds.has(edge.from) || !packageIds.has(edge.to)) add('runtime graph edge is outside the verified artifact package inventory')
    }
  }

  const peerContexts = evidence.artifact?.optionalPeerContexts
  for (const [packageName, peerName] of [['unhead', 'vite'], ['vue', 'typescript']]) {
    const packageEntry = evidencePackages?.find(pkg => pkg.name === packageName)
    const contextEntry = peerContexts?.find(item => item.package === `${packageName}@${packageEntry?.version}`)
    const context = contextEntry?.contexts?.find(item => item.name === peerName)
    if (!packageEntry || !context) add(`verified ${packageName}/${peerName} optional peer context is missing`)
    else {
      if (!context.absentFromArtifact || !context.noRuntimeImport) add(`${packageName}/${peerName} optional peer disposition changed`)
      if (!context.lockSnapshot || !context.workspaceStoreKey?.includes(`${peerName}@${context.version}`)) add(`${packageName}/${peerName} peer resolution context is not tied to the frozen graph`)
    }
  }

  const artifactPackageNames = [
    ...(inventory?.packages ?? []).map(pkg => pkg.name),
    ...(evidenceBundleProvenance?.packages ?? []).map(pkg => pkg.name),
  ]
  if (artifactPackageNames.some(name => ['node-forge', 'listhen'].includes(name))) {
    add('node-forge/listhen entered the production artifact; dependency exposure requires re-plan')
  }
  return {
    failures,
    artifactDigest: actualTree?.digest ?? null,
    packageCount: inventory?.packages.length ?? null,
    bundlePackages: evidenceBundleProvenance?.packages ?? [],
  }
}

export function validateAuditResult(result, report) {
  const failures = []
  const advisories = report?.advisories
  const vulnerabilities = report?.metadata?.vulnerabilities
  const severities = ['info', 'low', 'moderate', 'high', 'critical']
  if (!report || typeof report !== 'object' || Array.isArray(report)
    || !advisories || typeof advisories !== 'object' || Array.isArray(advisories)
    || !vulnerabilities || typeof vulnerabilities !== 'object'
    || !severities.every(severity => Number.isInteger(vulnerabilities[severity]))) {
    return { valid: false, failures: ['pnpm audit returned an incomplete or error-shaped JSON report'], advisories: [] }
  }
  const values = Object.values(advisories)
  for (const advisory of values) {
    if (typeof advisory.module_name !== 'string'
      || !severities.includes(advisory.severity)
      || !Array.isArray(advisory.findings)
      || advisory.findings.length === 0) {
      failures.push('pnpm audit returned a malformed advisory entry')
      continue
    }
    if (advisory.findings.some(finding => typeof finding.version !== 'string'
      || !Array.isArray(finding.paths)
      || finding.paths.length === 0
      || finding.paths.some(dependencyPath => typeof dependencyPath !== 'string' || dependencyPath.length === 0))) {
      failures.push(`pnpm audit returned incomplete finding details for ${advisory.module_name}`)
    }
  }
  if (result?.error || result?.signal || (result?.status !== 0 && !(result?.status === 1 && values.length > 0))) {
    failures.push(`pnpm audit command failed${result?.signal ? ` with signal ${result.signal}` : ` with exit code ${result?.status}`}`)
  }
  if (result?.status === 0 && values.length > 0) failures.push('pnpm audit status contradicted a non-empty advisory report')
  if (result?.status === 1 && values.length === 0) failures.push('pnpm audit status contradicted an empty advisory report')
  return { valid: failures.length === 0, failures, advisories: values }
}

export function projectAdvisories(report, artifactPackages, bundledPackages = []) {
  const packageVersions = new Map()
  for (const pkg of [...artifactPackages, ...bundledPackages]) {
    const versions = packageVersions.get(pkg.name) ?? new Set()
    versions.add(pkg.version)
    packageVersions.set(pkg.name, versions)
  }
  const findings = []
  for (const [id, advisory] of Object.entries(report?.advisories ?? {})) {
    for (const finding of advisory.findings ?? []) {
      if (!packageVersions.get(advisory.module_name)?.has(finding.version)) continue
      findings.push({
        id: String(id),
        moduleName: advisory.module_name,
        version: finding.version,
        severity: advisory.severity,
        title: advisory.title,
        url: advisory.url,
        githubAdvisoryId: advisory.github_advisory_id,
        paths: finding.paths,
      })
    }
  }
  return findings
}

export function gateFailures({ auditValidation, evidenceValidation, artifactFindings, smoke }) {
  return [
    ...(auditValidation?.failures ?? []),
    ...(evidenceValidation?.failures ?? []),
    ...(artifactFindings ?? []).map(finding => `runtime artifact contains affected ${finding.moduleName}@${finding.version}`),
    ...(smoke?.failures ?? []),
  ]
}

async function unusedPort() {
  const server = createServer()
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  const port = server.address().port
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()))
  return port
}

function delay(milliseconds) {
  return new Promise(resolve => setTimeout(resolve, milliseconds))
}

export async function smokeRuntimeArtifact(outputDirectory, { timeoutMs = 30000 } = {}) {
  const serverDirectory = path.join(outputDirectory, 'server')
  const entrypoint = path.join(serverDirectory, 'index.mjs')
  const before = await treeDigest(outputDirectory)
  const port = await unusedPort()
  const smokeEnvironment = {
    PATH: process.env.PATH ?? '',
    HOME: process.env.HOME ?? '',
    NODE_ENV: 'production',
    NUXT_APP_ENV: 'production',
    PORT: String(port),
    NITRO_PORT: String(port),
    HOST: '127.0.0.1',
    NITRO_HOST: '127.0.0.1',
  }
  if (process.env.TMPDIR) smokeEnvironment.TMPDIR = process.env.TMPDIR
  const child = spawn(process.execPath, [entrypoint], {
    cwd: serverDirectory,
    env: smokeEnvironment,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let childOutput = ''
  child.stdout.setEncoding('utf8').on('data', chunk => { childOutput = `${childOutput}${chunk}`.slice(-12000) })
  child.stderr.setEncoding('utf8').on('data', chunk => { childOutput = `${childOutput}${chunk}`.slice(-12000) })
  let exit = null
  child.once('exit', (code, signal) => { exit = { code, signal } })
  let responseDetails = null
  const expires = Date.now() + timeoutMs
  while (Date.now() < expires) {
    if (exit) break
    try {
      const response = await fetch(`http://127.0.0.1:${port}/`, { signal: AbortSignal.timeout(1500) })
      const body = await response.arrayBuffer()
      responseDetails = {
        status: response.status,
        contentType: response.headers.get('content-type') ?? '',
        bodyBytes: body.byteLength,
      }
      break
    } catch {
      await delay(250)
    }
  }
  let cleanupError = null
  if (!exit) {
    child.kill('SIGTERM')
    const stopped = await Promise.race([
      new Promise(resolve => child.once('exit', () => resolve(true))),
      delay(4000).then(() => false),
    ])
    if (!stopped) {
      child.kill('SIGKILL')
      await Promise.race([new Promise(resolve => child.once('exit', resolve)), delay(1000)])
      cleanupError = 'runtime smoke process required SIGKILL after SIGTERM'
    }
  }
  const after = await treeDigest(outputDirectory)
  const failures = []
  if (!responseDetails) failures.push(exit ? `production server exited before responding (${exit.code ?? exit.signal})` : 'production server did not become ready before timeout')
  else {
    if (responseDetails.status !== 200) failures.push(`production GET / returned HTTP ${responseDetails.status}`)
    if (!responseDetails.contentType.includes('text/html')) failures.push('production GET / did not return HTML')
    if (responseDetails.bodyBytes < 1) failures.push('production GET / returned an empty body')
  }
  if (cleanupError) failures.push(cleanupError)
  if (before.digest !== after.digest) failures.push('production smoke changed the .output artifact contents')
  if (exit && exit.code !== 0 && responseDetails) failures.push(`production server exited with code ${exit.code} after responding`)
  return {
    passed: failures.length === 0,
    failures,
    artifactSha256Before: before.digest,
    artifactSha256After: after.digest,
    response: responseDetails,
    output: childOutput,
  }
}

async function writeReport(reportPath, report) {
  await writeFile(reportPath, `${JSON.stringify(report, null, 2)}\n`)
}

export async function runRuntimeArtifactGate() {
  const outputDirectory = path.resolve(process.env.RUNTIME_ARTIFACT_DIRECTORY ?? path.join(root, 'apps/web-console/.output'))
  const evidencePath = path.resolve(process.env.RUNTIME_ARTIFACT_EVIDENCE ?? path.join(root, '.runtime-evidence.json'))
  const reportPath = path.resolve(process.env.RUNTIME_AUDIT_REPORT ?? path.join(root, 'runtime-audit-report.json'))
  const lockPath = path.join(root, 'pnpm-lock.yaml')
  let evidence = null
  let evidenceReadError = null
  try {
    evidence = JSON.parse(await readFile(evidencePath, 'utf8'))
  } catch (error) {
    evidenceReadError = error.message
  }

  const auditResult = spawnSync('corepack', ['pnpm', 'audit', '--prod', '--json'], {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 20 * 1024 * 1024,
  })
  let workspaceReport = null
  let auditParseError = null
  try {
    workspaceReport = JSON.parse(auditResult.stdout)
  } catch (error) {
    auditParseError = error.message
  }
  const auditValidation = validateAuditResult(auditResult, workspaceReport)
  const evidenceValidation = evidenceReadError
    ? {
        failures: [
          `runtime evidence cannot be read: ${evidenceReadError}`,
          ...(process.env.RUNTIME_ARTIFACT_DOWNLOAD_OUTCOME && process.env.RUNTIME_ARTIFACT_DOWNLOAD_OUTCOME !== 'success'
            ? ['same-run production artifact download failed']
            : []),
        ],
        artifactDigest: null,
        packageCount: null,
        bundlePackages: [],
      }
    : await verifyRuntimeEvidence({
        outputDirectory,
        evidence,
        lockPath,
        expectedCommit: process.env.GITHUB_SHA,
        expectedRunId: process.env.GITHUB_RUN_ID,
        expectedRunAttempt: process.env.GITHUB_RUN_ATTEMPT,
      })
  const artifactFindings = evidenceValidation.failures.length === 0
    ? projectAdvisories(workspaceReport, evidence.artifact.packages, evidenceValidation.bundlePackages)
    : []
  const workspaceFindings = Object.values(workspaceReport?.advisories ?? {}).map(advisory => ({
    moduleName: advisory.module_name,
    severity: advisory.severity,
    title: advisory.title,
    url: advisory.url,
    githubAdvisoryId: advisory.github_advisory_id,
    findings: advisory.findings,
  }))

  const report = {
    schemaVersion: 1,
    source: evidence?.source ?? null,
    artifact: {
      sha256: evidenceValidation.artifactDigest,
      packageCount: evidenceValidation.packageCount,
      bundlePackageCount: evidenceValidation.bundlePackages.length,
      evidenceSha256: evidence ? sha256(JSON.stringify(evidence)) : null,
    },
    workspaceAudit: {
      command: 'corepack pnpm audit --prod --json',
      status: auditResult.status,
      signal: auditResult.signal,
      stderr: auditResult.stderr?.trim() || null,
      parseError: auditParseError,
      validationFailures: auditValidation.failures,
      findings: workspaceFindings,
      raw: workspaceReport,
      statement: workspaceFindings.some(item => item.moduleName === 'node-forge')
        ? 'node-forge remains an unresolved workspace dependency finding; this gate does not remediate or suppress it.'
        : 'This is the complete workspace dependency audit report.',
    },
    runtimeArtifactAudit: {
      evidenceFailures: evidenceValidation.failures,
      findings: artifactFindings,
      blocked: artifactFindings.length > 0,
    },
    runtimeSmoke: { status: 'not-run', failures: [] },
    gate: { passed: false, failures: [] },
  }
  await writeReport(reportPath, report)

  for (const advisory of workspaceFindings) {
    console.log(`[workspace] ${advisory.severity}: ${advisory.moduleName}: ${advisory.title}`)
    for (const finding of advisory.findings ?? []) {
      for (const dependencyPath of finding.paths ?? []) console.log(`  path: ${dependencyPath}`)
    }
  }
  if (workspaceFindings.some(item => item.moduleName === 'node-forge')) {
    console.log('The node-forge workspace advisory remains unresolved and is not described as fixed or suppressed.')
  }
  if (auditResult.stderr?.trim()) process.stderr.write(`${auditResult.stderr.trim()}\n`)
  for (const failure of [...auditValidation.failures, ...evidenceValidation.failures]) console.error(`FAIL: ${failure}`)
  for (const finding of artifactFindings) {
    console.error(`FAIL: runtime artifact contains affected ${finding.moduleName}@${finding.version} (${finding.severity}, ${finding.githubAdvisoryId ?? finding.id})`)
  }

  if (auditValidation.valid && evidenceValidation.failures.length === 0) {
    const smoke = await smokeRuntimeArtifact(outputDirectory)
    const smokeFailures = [...smoke.failures]
    if (smoke.artifactSha256Before !== evidenceValidation.artifactDigest) smokeFailures.push('runtime smoke did not start from the audited artifact hash')
    if (smoke.artifactSha256After !== evidenceValidation.artifactDigest) smokeFailures.push('runtime smoke did not leave the audited artifact hash unchanged')
    report.runtimeSmoke = {
      status: smokeFailures.length === 0 ? 'passed' : 'failed',
      failures: smokeFailures,
      artifactSha256Before: smoke.artifactSha256Before,
      artifactSha256After: smoke.artifactSha256After,
      response: smoke.response,
      output: smokeFailures.length === 0 ? undefined : smoke.output,
    }
    for (const failure of smokeFailures) console.error(`FAIL: runtime smoke: ${failure}`)
  } else {
    report.runtimeSmoke = { status: 'skipped', failures: ['evidence validation or advisory source validation failed'] }
  }

  const failures = gateFailures({ auditValidation, evidenceValidation, artifactFindings, smoke: report.runtimeSmoke })
  report.gate = { passed: failures.length === 0, failures }
  await writeReport(reportPath, report)
  if (failures.length > 0) {
    console.error(`Runtime artifact dependency gate failed with ${failures.length} failure(s). Full workspace report: ${path.relative(root, reportPath)}`)
    process.exitCode = 1
    return
  }
  console.log(`Runtime artifact dependency gate passed for ${evidenceValidation.packageCount} physical packages and ${evidenceValidation.bundlePackages.length} bundled package contexts across ${evidence.artifact.bundleProvenance.chunkCount} chunks; artifact SHA-256 ${evidenceValidation.artifactDigest}.`)
  console.log('The complete workspace audit report is retained separately; the node-forge workspace advisory remains unresolved.')
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  await runRuntimeArtifactGate().catch(async (error) => {
    const reportPath = path.resolve(process.env.RUNTIME_AUDIT_REPORT ?? path.join(root, 'runtime-audit-report.json'))
    try {
      await writeReport(reportPath, { schemaVersion: 1, gate: { passed: false, failures: [error.message] } })
    } catch {}
    console.error(`runtime artifact dependency gate failed closed: ${error.message}`)
    process.exitCode = 1
  })
}
