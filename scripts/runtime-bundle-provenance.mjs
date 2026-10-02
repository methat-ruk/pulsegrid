import { createHash } from 'node:crypto'
import { readFile, readdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const packageStoreMarker = '/node_modules/.pnpm/'
const exactVersionPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/
const javascriptExtensions = new Set(['.cjs', '.js', '.mjs'])
const provenanceFile = '.runtime-bundle-provenance.json'
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const applicationRoot = path.join(repositoryRoot, 'apps/web-console')
const buildProvenancePath = path.join(applicationRoot, provenanceFile)

function sha256(value) {
  return createHash('sha256').update(value).digest('hex')
}

function parsePackageModuleId(moduleId) {
  const normalized = moduleId.replaceAll('\\', '/')
  const markerIndex = normalized.lastIndexOf(packageStoreMarker)
  const storePathIndex = markerIndex >= 0
    ? markerIndex + packageStoreMarker.length
    : normalized.startsWith('node_modules/.pnpm/')
      ? 'node_modules/.pnpm/'.length
      : -1
  if (storePathIndex < 0) return null

  const storePath = normalized.slice(storePathIndex).split(/[?#]/, 1)[0]
  const storeKey = storePath.split('/')[0]
  const packageDirectoryMarker = '/node_modules/'
  const packageDirectoryIndex = storePath.indexOf(packageDirectoryMarker)
  if (!storeKey || packageDirectoryIndex < 0) throw new Error(`incomplete pnpm module identity: ${moduleId}`)

  const packagePath = storePath.slice(packageDirectoryIndex + packageDirectoryMarker.length)
  const parts = packagePath.split('/').filter(Boolean)
  const scoped = parts[0]?.startsWith('@') === true
  const packagePartCount = scoped ? 2 : 1
  if (parts.length <= packagePartCount) throw new Error(`pnpm module identity has no package-relative path: ${moduleId}`)

  const name = parts.slice(0, packagePartCount).join('/')
  const sourceFile = parts.slice(packagePartCount).join('/')
  const encodedName = name.replaceAll('/', '+')
  const versionPrefix = `${encodedName}@`
  if (!storeKey.startsWith(versionPrefix)) throw new Error(`pnpm module name does not match its store key: ${moduleId}`)
  const version = storeKey.slice(versionPrefix.length).split('_', 1)[0]
  if (!exactVersionPattern.test(version)) throw new Error(`pnpm module has no exact version: ${moduleId}`)
  if (sourceFile.split('/').some(part => part === '..' || part === '.')) {
    throw new Error(`pnpm module path escapes its package: ${moduleId}`)
  }
  return { name, version, storeKey, sourceFile }
}

function classifyModuleId(moduleId) {
  if (typeof moduleId !== 'string' || moduleId.length === 0) throw new Error('Nitro emitted a module without an identity')
  const packageIdentity = parsePackageModuleId(moduleId)
  if (packageIdentity) {
    const { storeKey, ...identity } = packageIdentity
    return { kind: 'package', ...identity, sourceVirtualStoreKey: storeKey }
  }

  const normalized = moduleId.replaceAll('\\', '/')
  if (normalized.includes('/node_modules/') && !normalized.includes('/node_modules/.cache/nuxt/')) {
    throw new Error(`Nitro emitted an external module without a frozen pnpm package identity: ${moduleId}`)
  }
  if (normalized.startsWith('virtual:') || normalized.startsWith('\0')) {
    return { kind: 'virtual', idSha256: sha256(moduleId) }
  }

  const cleanId = normalized.split(/[?#]/, 1)[0]
  const absolutePath = cleanId.startsWith('file://')
    ? fileURLToPath(cleanId)
    : path.resolve(repositoryRoot, cleanId)
  const relativeToApp = path.relative(applicationRoot, absolutePath)
  if (relativeToApp.startsWith('..') || path.isAbsolute(relativeToApp)) {
    throw new Error(`Nitro emitted a module outside the application or frozen pnpm graph: ${moduleId}`)
  }
  const sourceFile = relativeToApp.split(path.sep).join('/')
  if (sourceFile.includes('/node_modules/.cache/nuxt/')) {
    return { kind: 'generated', idSha256: sha256(moduleId) }
  }
  return { kind: 'application', sourceFile }
}

export function createRuntimeBundleProvenancePlugin() {
  return {
    name: 'pulsegrid-runtime-bundle-provenance',
    async generateBundle(_outputOptions, bundle) {
      const chunks = Object.values(bundle)
        .filter(output => output.type === 'chunk')
        .map(output => ({
          file: path.posix.normalize(output.fileName),
          modules: Object.keys(output.modules).sort().map(classifyModuleId),
          ...(Object.keys(output.modules).length === 0 ? { attribution: 'nitro-entry-wrapper' } : {}),
        }))
        .sort((left, right) => left.file.localeCompare(right.file))
      if (chunks.length === 0) throw new Error('Nitro produced no chunks for runtime package provenance')
      const unsupportedEmptyChunk = chunks.find(chunk => chunk.modules.length === 0
        && (chunk.file !== 'index.mjs' || chunk.attribution !== 'nitro-entry-wrapper'))
      if (unsupportedEmptyChunk) throw new Error(`Nitro chunk has no attributable module provenance: ${unsupportedEmptyChunk.file}`)

      await writeFile(buildProvenancePath, `${JSON.stringify({ format: 'nitro-rollup-modules-v1', chunks }, null, 2)}\n`)
    },
  }
}

export function attachRuntimeBundleProvenance(_nitro, rollupConfig) {
  rollupConfig.plugins ??= []
  rollupConfig.plugins.push(createRuntimeBundleProvenancePlugin())
}

async function listFiles(directory, prefix = '') {
  const files = []
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const relative = prefix ? `${prefix}/${entry.name}` : entry.name
    if (entry.name === 'node_modules') continue
    if (entry.isDirectory()) files.push(...await listFiles(path.join(directory, entry.name), relative))
    else if (entry.isFile()) files.push(relative)
  }
  return files.sort()
}

function sourceMapReference(moduleSource) {
  const references = [...moduleSource.matchAll(/\/\/[#@]\s*sourceMappingURL=([^\s]+)/g)]
  if (references.length !== 1) throw new Error('runtime bundle module does not have exactly one external source map reference')
  const reference = references[0][1]
  if (!reference || reference.startsWith('data:') || /^[a-z][a-z0-9+.-]*:/i.test(reference)) {
    throw new Error('runtime bundle source map reference is not a local file')
  }
  return reference
}

function sourceMapPackageIdentity(source, sourceRoot = '') {
  const combinedSource = sourceRoot ? path.posix.join(sourceRoot, source) : source
  const identity = parsePackageModuleId(combinedSource)
  if (identity) return identity

  const normalized = combinedSource.replaceAll('\\', '/')
  if (normalized.includes('node_modules/') && !normalized.includes('node_modules/.cache/nuxt/')) {
    throw new Error(`source map has an unrecognized external package source: ${source}`)
  }
  if (normalized.includes('virtual:nuxt:')
    || normalized.includes('/app/')
    || normalized.includes('/server/')
    || normalized.includes('/shared/')
    || normalized.includes('/node_modules/.cache/nuxt/')) return null
  throw new Error(`source map source is neither application code nor a frozen package: ${source}`)
}

function validModule(module) {
  if (!module || !['application', 'generated', 'package', 'virtual'].includes(module.kind)) return false
  if (module.kind === 'package') {
    const encodedName = typeof module.name === 'string' ? module.name.replaceAll('/', '+') : ''
    return encodedName.length > 0
      && exactVersionPattern.test(module.version ?? '')
      && (module.sourceVirtualStoreKey === `${encodedName}@${module.version}`
        || module.sourceVirtualStoreKey?.startsWith(`${encodedName}@${module.version}_`) === true)
      && typeof module.sourceFile === 'string'
      && module.sourceFile.length > 0
      && !module.sourceFile.split('/').some(part => part === '..' || part === '.')
  }
  if (module.kind === 'application') {
    return typeof module.sourceFile === 'string'
      && module.sourceFile.length > 0
      && !module.sourceFile.split('/').some(part => part === '..' || part === '.')
  }
  return /^[a-f0-9]{64}$/.test(module.idSha256 ?? '')
}

export async function collectRuntimeBundleProvenance(outputDirectory, metadataPath = path.join(path.dirname(outputDirectory), provenanceFile)) {
  const serverDirectory = path.join(outputDirectory, 'server')
  const metadata = JSON.parse(await readFile(metadataPath, 'utf8'))
  if (metadata.format !== 'nitro-rollup-modules-v1' || !Array.isArray(metadata.chunks) || metadata.chunks.length === 0) {
    throw new Error('Nitro Rollup module provenance metadata is missing or malformed')
  }

  const serverFiles = await listFiles(serverDirectory)
  const emittedChunks = serverFiles
    .filter(file => javascriptExtensions.has(path.extname(file).toLowerCase()))
    .sort()
  const sourceMapFiles = new Set(serverFiles.filter(file => file.endsWith('.map')))
  const recordedChunks = metadata.chunks.map(chunk => chunk.file).sort()
  if (JSON.stringify(recordedChunks) !== JSON.stringify(emittedChunks)) {
    throw new Error('Nitro Rollup chunk inventory differs from the production JavaScript artifact')
  }

  const packages = new Map()
  const referencedMaps = new Set()
  let moduleCount = 0
  const verifiedChunks = []
  for (const chunk of metadata.chunks) {
    if (typeof chunk.file !== 'string' || !Array.isArray(chunk.modules)) {
      throw new Error('Nitro Rollup chunk module metadata is incomplete')
    }
    if (chunk.modules.length === 0
      && (chunk.file !== 'index.mjs' || chunk.attribution !== 'nitro-entry-wrapper')) {
      throw new Error(`Nitro chunk has no attributable module provenance: ${chunk.file}`)
    }
    moduleCount += chunk.modules.length
    const packageModules = new Map()
    for (const module of chunk.modules) {
      if (!validModule(module)) throw new Error(`Nitro Rollup module metadata is invalid in ${chunk.file}`)
      if (module.kind !== 'package') continue
      packageModules.set(`${module.name}@${module.version}\0${module.sourceVirtualStoreKey}\0${module.sourceFile}`, {
        name: module.name,
        version: module.version,
        storeKey: module.sourceVirtualStoreKey,
        sourceFile: module.sourceFile,
      })
      const key = `${module.name}@${module.version}\0${module.sourceVirtualStoreKey}`
      const packageEvidence = packages.get(key) ?? {
        name: module.name,
        version: module.version,
        sourceVirtualStoreKey: module.sourceVirtualStoreKey,
        modules: [],
      }
      packageEvidence.modules.push({ chunk: chunk.file, sourceFile: module.sourceFile, origin: 'rollup' })
      packages.set(key, packageEvidence)
    }

    const modulePath = path.join(serverDirectory, chunk.file)
    const reference = sourceMapReference(await readFile(modulePath, 'utf8'))
    const mapPath = path.resolve(path.dirname(modulePath), reference)
    const relativeMapPath = path.relative(serverDirectory, mapPath)
    if (relativeMapPath.startsWith('..') || path.isAbsolute(relativeMapPath)) {
      throw new Error(`runtime bundle source map escapes the server artifact: ${chunk.file}`)
    }
    const mapFile = relativeMapPath.split(path.sep).join('/')
    if (!sourceMapFiles.has(mapFile)) throw new Error(`runtime bundle source map is missing from the artifact: ${chunk.file}`)
    if (referencedMaps.has(mapFile)) throw new Error(`multiple bundle chunks share one source map: ${mapFile}`)
    referencedMaps.add(mapFile)

    const sourceMap = JSON.parse(await readFile(mapPath, 'utf8'))
    if (sourceMap.version !== 3
      || sourceMap.file !== path.basename(chunk.file)
      || !Array.isArray(sourceMap.sources)
      || typeof sourceMap.mappings !== 'string') {
      throw new Error(`runtime bundle source map is malformed or mismatched: ${mapFile}`)
    }
    if (sourceMap.sources.length === 0 && chunk.modules.length > 0 && packageModules.size === 0) {
      const isEmptyNuxtSpaTemplate = chunk.file === 'chunks/virtual/_virtual_spa-template.mjs'
        && /^const template = "";\s*export \{ template \};\s*\/\/# sourceMappingURL=/.test(await readFile(modulePath, 'utf8'))
      if (!isEmptyNuxtSpaTemplate) {
        throw new Error(`runtime bundle source map has no sources and no direct package provenance: ${chunk.file}`)
      }
    }
    const mappedPackages = new Map()
    for (const source of sourceMap.sources) {
      if (typeof source !== 'string' || source.length === 0) throw new Error(`runtime bundle source map has an invalid source: ${mapFile}`)
      const identity = sourceMapPackageIdentity(source, sourceMap.sourceRoot ?? '')
      if (!identity) continue
      const key = `${identity.name}@${identity.version}\0${identity.storeKey}\0${identity.sourceFile}`
      mappedPackages.set(key, identity)
    }
    for (const identity of mappedPackages.values()) {
      const key = `${identity.name}@${identity.version}\0${identity.storeKey}`
      const packageEvidence = packages.get(key) ?? {
        name: identity.name,
        version: identity.version,
        sourceVirtualStoreKey: identity.storeKey,
        modules: [],
      }
      packageEvidence.modules.push({ chunk: chunk.file, sourceFile: identity.sourceFile, origin: 'source-map' })
      packages.set(key, packageEvidence)
    }
    verifiedChunks.push({
      ...chunk,
      sourceMap: {
        file: mapFile,
        sourceCount: sourceMap.sources.length,
        packages: [...mappedPackages.values()].sort((left, right) => `${left.name}@${left.version}:${left.sourceFile}`.localeCompare(`${right.name}@${right.version}:${right.sourceFile}`)),
      },
    })
  }
  if (moduleCount === 0) throw new Error('Nitro Rollup output has no attributable modules')
  if (referencedMaps.size !== sourceMapFiles.size) {
    const unreferenced = [...sourceMapFiles].filter(file => !referencedMaps.has(file))
    throw new Error(`production artifact contains unreferenced source maps: ${unreferenced.join(', ')}`)
  }

  const packageEvidence = [...packages.values()]
    .map(item => ({
      ...item,
      modules: [...new Map(item.modules.map(module => [`${module.chunk}\0${module.sourceFile}\0${module.origin}`, module])).values()]
        .sort((left, right) => `${left.chunk}\0${left.sourceFile}\0${left.origin}`.localeCompare(`${right.chunk}\0${right.sourceFile}\0${right.origin}`)),
    }))
    .sort((left, right) => `${left.name}@${left.version}\0${left.sourceVirtualStoreKey}`.localeCompare(`${right.name}@${right.version}\0${right.sourceVirtualStoreKey}`))

  return {
    format: 'nitro-rollup-and-sourcemap-v1',
    moduleCount,
    chunkCount: metadata.chunks.length,
    sourceMapCount: referencedMaps.size,
    chunks: verifiedChunks,
    packages: packageEvidence,
  }
}
