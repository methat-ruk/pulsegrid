import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, rm, unlink, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import {
  projectAdvisories,
  gateFailures,
  sha256,
  smokeRuntimeArtifact,
  stable,
  treeDigest,
  validateAuditResult,
  verifyRuntimeEvidence,
} from './runtime-artifact-gate.mjs'

async function fixture() {
  const root = await mkdtemp(path.join(os.tmpdir(), 'pulsegrid-runtime-gate-test-'))
  const output = path.join(root, '.output')
  const server = path.join(output, 'server')
  const packageDirectory = path.join(server, 'node_modules/safe-package')
  await mkdir(packageDirectory, { recursive: true })
  await writeFile(path.join(server, 'package.json'), JSON.stringify({ dependencies: { 'safe-package': '1.0.0' } }))
  await writeFile(path.join(server, 'index.mjs'), 'process.exit(1)\n//# sourceMappingURL=index.mjs.map\n')
  await writeFile(path.join(server, 'index.mjs.map'), JSON.stringify({
    version: 3,
    file: 'index.mjs',
    sources: ['../../../node_modules/.pnpm/safe-package@1.0.0/node_modules/safe-package/index.js'],
    names: [],
    mappings: 'AAAA',
  }))
  const bundlePackageModule = {
    kind: 'package',
    name: 'safe-package',
    version: '1.0.0',
    sourceFile: 'index.js',
    sourceVirtualStoreKey: 'safe-package@1.0.0',
  }
  await writeFile(path.join(root, '.runtime-bundle-provenance.json'), JSON.stringify({
    format: 'nitro-rollup-modules-v1',
    chunks: [{ file: 'index.mjs', modules: [bundlePackageModule] }],
  }))
  await writeFile(path.join(packageDirectory, 'package.json'), JSON.stringify({ name: 'safe-package', version: '1.0.0' }))
  await writeFile(path.join(packageDirectory, 'index.js'), 'export default 1\n')
  const packageManifestMetadata = {
    name: 'safe-package',
    version: '1.0.0',
    dependencies: {},
    optionalDependencies: {},
    peerDependencies: {},
    peerDependenciesMeta: {},
  }
  const packageTree = await treeDigest(packageDirectory)
  const sourceFileSha256 = sha256(await readFile(path.join(packageDirectory, 'index.js')))
  const outputTree = await treeDigest(output)
  const runtimeManifest = await readFile(path.join(server, 'package.json'))
  const lockText = 'frozen lock fixture\n'
  const evidence = {
    schemaVersion: 2,
    source: {
      commit: 'test-commit',
      runId: '42',
      runAttempt: '1',
      lockSha256: sha256(lockText),
      nodeVersion: '24.20.0',
      pnpmVersion: '12.3.4',
      buildCommand: 'corepack pnpm run web:build',
      productionImporter: 'apps/web-console',
      lockfileVersion: '9.0',
    },
    artifact: {
      path: 'apps/web-console/.output',
      sha256: outputTree.digest,
      fileCount: outputTree.fileCount,
      runtimeManifestSha256: sha256(runtimeManifest),
      packages: [{
        name: 'safe-package',
        version: '1.0.0',
        packageManifestSha256: sha256(stable(packageManifestMetadata)),
        artifactTree: packageTree,
        sourceVirtualStoreKey: 'safe-package@1.0.0',
        sourceNonManifestFileProof: { nonManifestFileCount: 1, nonManifestDigest: sha256('verified') },
        lock: { integrity: 'sha512-YWJjZA==', snapshotKeys: ['safe-package@1.0.0'] },
      }],
      bundleProvenance: {
        format: 'nitro-rollup-and-sourcemap-v1',
        moduleCount: 1,
        chunkCount: 1,
        sourceMapCount: 1,
        chunks: [{
          file: 'index.mjs',
          modules: [bundlePackageModule],
          sourceMap: {
            file: 'index.mjs.map',
            sourceCount: 1,
            packages: [{ name: 'safe-package', version: '1.0.0', storeKey: 'safe-package@1.0.0', sourceFile: 'index.js' }],
          },
        }],
        packages: [{
          name: 'safe-package',
          version: '1.0.0',
          sourceVirtualStoreKey: 'safe-package@1.0.0',
          modules: [
            { chunk: 'index.mjs', sourceFile: 'index.js', origin: 'rollup' },
            { chunk: 'index.mjs', sourceFile: 'index.js', origin: 'source-map' },
          ],
          packageManifestSha256: sha256(stable(packageManifestMetadata)),
          sourceFileProofs: [
            { chunk: 'index.mjs', sourceFile: 'index.js', origin: 'rollup', sha256: sourceFileSha256 },
            { chunk: 'index.mjs', sourceFile: 'index.js', origin: 'source-map', sha256: sourceFileSha256 },
          ],
          lock: { integrity: 'sha512-YWJjZA==', packageRecords: ['safe-package@1.0.0'], snapshotKeys: ['safe-package@1.0.0'] },
          lockVerified: true,
        }],
      },
      graph: {
        entry: 'apps/web-console/.output/server/index.mjs',
        moduleCount: 1,
        packageEdges: [{ from: 'safe-package@1.0.0', to: 'safe-package@1.0.0', specifier: 'safe-package', lockVerified: true }],
        unresolved: [],
        nonLiteral: [],
        sourceGraphNodeCount: 2,
        sourceGraphEdgeCount: 1,
        allEdgesInFrozenProductionGraph: true,
      },
      optionalPeerContexts: [
        { package: 'unhead@3.4.0', contexts: [{ name: 'vite', version: '8.2.2', lockSnapshot: 'unhead@3.4.0(vite@8.2.2)', workspaceStoreKey: 'unhead@3.4.0_vite@8.2.2', absentFromArtifact: true, noRuntimeImport: true }] },
        { package: 'vue@3.5.42', contexts: [{ name: 'typescript', version: '5.9.3', lockSnapshot: 'vue@3.5.42(typescript@5.9.3)', workspaceStoreKey: 'vue@3.5.42_typescript@5.9.3', absentFromArtifact: true, noRuntimeImport: true }] },
      ],
    },
  }
  await writeFile(path.join(root, 'pnpm-lock.yaml'), lockText)
  return { root, output, evidence }
}

test('runtime evidence fails closed when the evidence document is absent', async () => {
  const { root, output } = await fixture()
  try {
    const result = await verifyRuntimeEvidence({ outputDirectory: output, evidence: null, lockPath: path.join(root, 'pnpm-lock.yaml') })
    assert.match(result.failures.join('\n'), /missing or has an unsupported schema/)
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when downloaded artifact bytes differ', async () => {
  const { root, output, evidence } = await fixture()
  try {
    evidence.artifact.sha256 = '0'.repeat(64)
    const result = await verifyRuntimeEvidence({
      outputDirectory: output,
      evidence,
      lockPath: path.join(root, 'pnpm-lock.yaml'),
      expectedCommit: 'test-commit',
      expectedRunId: '42',
      expectedRunAttempt: '1',
    })
    assert.ok(result.failures.some(failure => failure.includes('downloaded .output hash does not match')))
    const mismatchedRun = await verifyRuntimeEvidence({
      outputDirectory: output,
      evidence: JSON.parse(JSON.stringify(evidence)),
      lockPath: path.join(root, 'pnpm-lock.yaml'),
      expectedCommit: 'test-commit',
      expectedRunId: '43',
      expectedRunAttempt: '1',
    })
    assert.ok(mismatchedRun.failures.some(failure => failure.includes('not produced by this workflow run')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when runtime graph proof is incomplete', async () => {
  const { root, output, evidence } = await fixture()
  try {
    evidence.artifact.graph.packageEdges[0].lockVerified = false
    const result = await verifyRuntimeEvidence({ outputDirectory: output, evidence, lockPath: path.join(root, 'pnpm-lock.yaml') })
    assert.ok(result.failures.some(failure => failure.includes('edge without frozen graph proof')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when same-run artifact download fails', async () => {
  const { root, output, evidence } = await fixture()
  try {
    const result = await verifyRuntimeEvidence({
      outputDirectory: output,
      evidence,
      lockPath: path.join(root, 'pnpm-lock.yaml'),
      downloadOutcome: 'failure',
    })
    assert.ok(result.failures.some(failure => failure.includes('same-run production artifact download failed')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when the adjacent Rollup provenance sidecar is absent', async () => {
  const { root, output, evidence } = await fixture()
  try {
    await unlink(path.join(root, '.runtime-bundle-provenance.json'))
    const result = await verifyRuntimeEvidence({ outputDirectory: output, evidence, lockPath: path.join(root, 'pnpm-lock.yaml') })
    assert.ok(result.failures.some(failure => failure.includes('artifact or metadata cannot be inspected')))
    assert.ok(result.failures.some(failure => failure.includes('bundled package provenance could not be read')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when adjacent Rollup provenance no longer matches the built chunks', async () => {
  const { root, output, evidence } = await fixture()
  try {
    const provenancePath = path.join(root, '.runtime-bundle-provenance.json')
    const provenance = JSON.parse(await readFile(provenancePath, 'utf8'))
    provenance.chunks[0].modules = [{ kind: 'application', sourceFile: 'index.mjs' }]
    await writeFile(provenancePath, JSON.stringify(provenance))
    const result = await verifyRuntimeEvidence({ outputDirectory: output, evidence, lockPath: path.join(root, 'pnpm-lock.yaml') })
    assert.ok(result.failures.some(failure => failure.includes('bundle source maps differ from frozen package provenance evidence')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime evidence fails closed when the audited bundle loses its source map', async () => {
  const { root, output, evidence } = await fixture()
  try {
    await unlink(path.join(output, 'server/index.mjs.map'))
    const result = await verifyRuntimeEvidence({ outputDirectory: output, evidence, lockPath: path.join(root, 'pnpm-lock.yaml') })
    assert.ok(result.failures.some(failure => failure.includes('source map is missing from the artifact')))
    assert.ok(result.failures.some(failure => failure.includes('downloaded .output hash does not match')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('workspace node-forge finding remains visible but does not match an artifact without that version', () => {
  const report = {
    advisories: {
      '1240912': {
        module_name: 'node-forge',
        severity: 'high',
        title: 'RSA signature verification advisory',
        url: 'https://github.com/advisories/GHSA-86w9-cpqp-85rv',
        github_advisory_id: 'GHSA-86w9-cpqp-85rv',
        findings: [{ version: '1.4.0', paths: ['nuxt>listhen>node-forge'] }],
      },
    },
  }
  assert.equal(Object.values(report.advisories).length, 1)
  const safeArtifactFindings = projectAdvisories(report, [{ name: 'safe-package', version: '1.0.0' }])
  assert.deepEqual(safeArtifactFindings, [])
  assert.deepEqual(gateFailures({
    auditValidation: { failures: [] },
    evidenceValidation: { failures: [] },
    artifactFindings: safeArtifactFindings,
    smoke: { failures: [] },
  }), [])

  const affectedArtifactFindings = projectAdvisories(report, [{ name: 'node-forge', version: '1.4.0' }])
  assert.equal(affectedArtifactFindings.length, 1)
  assert.ok(gateFailures({
    auditValidation: { failures: [] },
    evidenceValidation: { failures: [] },
    artifactFindings: affectedArtifactFindings,
    smoke: { failures: [] },
  }).some(failure => failure.includes('affected node-forge@1.4.0')))
})

test('an advisory for a package present only in a bundled chunk blocks the artifact gate', () => {
  const report = {
    advisories: {
      'synthetic-bundled-package': {
        module_name: 'h3',
        severity: 'high',
        title: 'Synthetic regression advisory for a package omitted from node_modules',
        url: 'https://example.invalid/synthetic-bundled-package',
        findings: [
          { version: '1.14.0', paths: ['apps__web-console>package-a>h3'] },
          { version: '1.15.11', paths: ['apps__web-console>nuxt>nitropack>h3'] },
        ],
      },
    },
  }
  const runtimePackages = [{ name: 'h3', version: '1.14.0' }]
  const bundledPackages = [{ name: 'h3', version: '1.15.11' }]
  const findings = projectAdvisories(report, runtimePackages, bundledPackages)
  assert.deepEqual(findings.map(finding => finding.version), ['1.14.0', '1.15.11'])
  const bundledOnlyFindings = projectAdvisories(report, [], bundledPackages)
  assert.deepEqual(bundledOnlyFindings.map(finding => finding.version), ['1.15.11'])
  assert.ok(gateFailures({
    auditValidation: { failures: [] },
    evidenceValidation: { failures: [] },
    artifactFindings: bundledOnlyFindings,
    smoke: { failures: [] },
  }).some(failure => failure.includes('affected h3@1.15.11')))
})

test('audit command output must be valid and consistent with its process status', () => {
  const report = {
    advisories: { '1': { module_name: 'node-forge', severity: 'high', findings: [{ version: '1.4.0', paths: ['nuxt>listhen>node-forge'] }] } },
    metadata: { vulnerabilities: { info: 0, low: 0, moderate: 0, high: 1, critical: 0 } },
  }
  assert.equal(validateAuditResult({ status: 1 }, report).valid, true)
  assert.equal(validateAuditResult({ status: 2 }, report).valid, false)
  assert.equal(validateAuditResult({ status: 0 }, null).valid, false)
  assert.equal(validateAuditResult({ status: 1 }, {
    ...report,
    advisories: { '1': { ...report.advisories['1'], findings: [] } },
  }).valid, false)
})

test('runtime smoke failure blocks the gate when the server exits before responding', async () => {
  const { root, output } = await fixture()
  try {
    const result = await smokeRuntimeArtifact(output, { timeoutMs: 5000 })
    assert.equal(result.passed, false)
    assert.ok(result.failures.some(failure => failure.includes('exited before responding')))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('runtime smoke failure blocks the gate if the process mutates the downloaded artifact', async () => {
  const { root, output } = await fixture()
  try {
    await writeFile(path.join(output, 'server/index.mjs'), `
      import { createServer } from 'node:http'
      import { writeFileSync } from 'node:fs'
      writeFileSync(new URL('../smoke-mutation.txt', import.meta.url), 'changed')
      createServer((request, response) => {
        response.writeHead(200, { 'content-type': 'text/html' })
        response.end('ok')
      }).listen(Number(process.env.PORT), process.env.HOST)
    `)
    const result = await smokeRuntimeArtifact(output, { timeoutMs: 5000 })
    assert.equal(result.passed, false)
    assert.ok(result.failures.some(failure => failure.includes('changed the .output artifact contents')))
    assert.ok(gateFailures({
      auditValidation: { failures: [] },
      evidenceValidation: { failures: [] },
      artifactFindings: [],
      smoke: { failures: result.failures },
    }).length > 0)
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})
