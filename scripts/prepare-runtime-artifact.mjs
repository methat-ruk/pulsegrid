import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { builtinModules, createRequire } from 'node:module'
import { lstat, readFile, readdir, realpath, readlink, stat, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { collectRuntimeBundleProvenance } from './runtime-bundle-provenance.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const outputDirectory = path.join(root, 'apps/web-console/.output')
const serverDirectory = path.join(outputDirectory, 'server')
const runtimeModulesDirectory = path.join(serverDirectory, 'node_modules')
const evidencePath = path.join(root, '.runtime-evidence.json')
const lockPath = path.join(root, 'pnpm-lock.yaml')
const packagePath = path.join(root, 'package.json')
let ts
let parseAllDocuments
function fail(message) {
  throw new Error(message)
}

function sha256(value) {
  return createHash('sha256').update(value).digest('hex')
}

function isExactVersion(value) {
  return typeof value === 'string' && /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(value)
}

function loadWorkspacePackage(name, version, entrypoint) {
  const encodedName = name.replaceAll('/', '+')
  const packageDirectory = path.join(root, 'node_modules/.pnpm', `${encodedName}@${version}`, 'node_modules', ...name.split('/'))
  const modulePath = path.join(packageDirectory, entrypoint)
  return createRequire(path.join(packageDirectory, 'package.json'))(modulePath)
}

function stable(value) {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${stable(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

async function listTree(directory, prefix = '') {
  const entries = []
  for (const name of (await readdir(directory)).sort()) {
    const absolute = path.join(directory, name)
    const relative = prefix ? `${prefix}/${name}` : name
    const info = await lstat(absolute)
    if (info.isSymbolicLink()) {
      entries.push({ path: relative, type: 'symlink', value: await readlink(absolute) })
    } else if (info.isDirectory()) {
      entries.push(...await listTree(absolute, relative))
    } else if (info.isFile()) {
      entries.push({ path: relative, type: 'file', value: await readFile(absolute) })
    } else {
      fail(`unsupported filesystem entry in runtime artifact: ${relative}`)
    }
  }
  return entries
}

async function treeDigest(directory) {
  const hash = createHash('sha256')
  const entries = await listTree(directory)
  for (const entry of entries) {
    hash.update(`${entry.type}\0${entry.path}\0${sha256(entry.value)}\n`)
  }
  return { digest: hash.digest('hex'), fileCount: entries.length }
}

async function packageFileProof(artifactPackageDirectory, sourcePackageDirectory) {
  const artifactFiles = await listTree(artifactPackageDirectory)
  const checked = []
  for (const entry of artifactFiles) {
    if (entry.path === 'package.json') continue
    if (entry.type !== 'file') fail(`unsupported package entry in ${artifactPackageDirectory}: ${entry.path}`)
    const sourcePath = path.join(sourcePackageDirectory, entry.path)
    let sourceContents
    try {
      sourceContents = await readFile(sourcePath)
    } catch {
      return null
    }
    if (!sourceContents.equals(entry.value)) return null
    checked.push({ path: entry.path, sha256: sha256(entry.value) })
  }
  checked.sort((left, right) => left.path.localeCompare(right.path))
  return {
    fileCount: artifactFiles.length,
    nonManifestFileCount: checked.length,
    nonManifestDigest: sha256(stable(checked)),
  }
}

function packageNameFromStorePath(packageDirectory) {
  const marker = `${path.sep}node_modules${path.sep}.pnpm${path.sep}`
  const index = packageDirectory.lastIndexOf(marker)
  if (index < 0) return null
  const afterStore = packageDirectory.slice(index + marker.length)
  return afterStore.split(`${path.sep}node_modules${path.sep}`)[0]
}

function packageRootFromModulesPath(modulesDirectory, name) {
  return path.join(modulesDirectory, ...name.split('/'))
}

function parseLockPackageId(key) {
  const packageId = key.split('(')[0]
  const separator = packageId.startsWith('@')
    ? packageId.indexOf('@', packageId.indexOf('/') + 1)
    : packageId.indexOf('@')
  if (separator < 1) return null
  return { name: packageId.slice(0, separator), version: packageId.slice(separator + 1) }
}

async function readArtifactPackages(serverManifest) {
  const dependencies = serverManifest.dependencies
  if (!dependencies || typeof dependencies !== 'object' || Array.isArray(dependencies)) {
    fail('Nitro server/package.json has no dependency map')
  }
  const packageNames = Object.keys(dependencies).sort()
  const actualDirectories = []
  for (const entry of await readdir(runtimeModulesDirectory, { withFileTypes: true })) {
    if (entry.name.startsWith('@') && entry.isDirectory()) {
      for (const scoped of await readdir(path.join(runtimeModulesDirectory, entry.name), { withFileTypes: true })) {
        if (scoped.isDirectory() || scoped.isSymbolicLink()) actualDirectories.push(`${entry.name}/${scoped.name}`)
      }
    } else if (entry.isDirectory() || entry.isSymbolicLink()) {
      actualDirectories.push(entry.name)
    }
  }
  actualDirectories.sort()
  if (stable(actualDirectories) !== stable(packageNames)) {
    fail(`Nitro dependency manifest and physical package directories differ (manifest=${packageNames.length}, physical=${actualDirectories.length})`)
  }

  const packages = []
  for (const name of packageNames) {
    const directory = packageRootFromModulesPath(runtimeModulesDirectory, name)
    const manifest = JSON.parse(await readFile(path.join(directory, 'package.json'), 'utf8'))
    const requestedVersion = dependencies[name]
    if (manifest.name !== name || manifest.version !== requestedVersion || !isExactVersion(requestedVersion)) {
      fail(`runtime package identity is not exact for ${name}: manifest=${manifest.name}@${manifest.version}, Nitro=${requestedVersion}`)
    }
    packages.push({
      name,
      version: manifest.version,
      directory,
      manifest,
      manifestMetadata: {
        name: manifest.name,
        version: manifest.version,
        dependencies: manifest.dependencies ?? {},
        optionalDependencies: manifest.optionalDependencies ?? {},
        peerDependencies: manifest.peerDependencies ?? {},
        peerDependenciesMeta: manifest.peerDependenciesMeta ?? {},
      },
    })
  }
  return packages
}

function readProjectLock(lockText) {
  const documents = parseAllDocuments(lockText)
  if (documents.some(document => document.errors.length > 0)) fail('pnpm-lock.yaml contains a YAML parse error')
  const project = documents.map(document => document.toJS()).find(document => document?.importers?.['apps/web-console'])
  if (!project || project.lockfileVersion !== '9.0') fail('the frozen pnpm project lock document is missing or unsupported')
  const packageRecords = new Map()
  for (const [key, value] of Object.entries(project.packages ?? {})) {
    const parsed = parseLockPackageId(key)
    if (!parsed) continue
    const current = packageRecords.get(`${parsed.name}@${parsed.version}`) ?? []
    current.push({ key, integrity: value?.resolution?.integrity ?? null, packageMetadata: value })
    packageRecords.set(`${parsed.name}@${parsed.version}`, current)
  }
  const snapshots = new Map()
  for (const [key, value] of Object.entries(project.snapshots ?? {})) {
    const parsed = parseLockPackageId(key)
    if (!parsed) continue
    const current = snapshots.get(`${parsed.name}@${parsed.version}`) ?? []
    current.push({ key, snapshot: value })
    snapshots.set(`${parsed.name}@${parsed.version}`, current)
  }
  return { project, packageRecords, snapshots }
}

function flattenProductionGraph(rootPackage) {
  const nodesByPath = new Map()
  const edges = new Set()
  const stack = [{ package: rootPackage, nameHint: rootPackage.name, parent: null }]
  while (stack.length > 0) {
    const current = stack.pop()
    const pkg = current.package
    const name = pkg.name ?? pkg.from ?? current.nameHint
    const nodePath = pkg.path ? path.resolve(pkg.path) : null
    if (nodePath && !nodesByPath.has(nodePath)) {
      nodesByPath.set(nodePath, {
        name,
        version: pkg.version ?? null,
        path: nodePath,
        virtualStoreKey: packageNameFromStorePath(nodePath),
      })
    }
    const node = nodePath ? nodesByPath.get(nodePath) : null
    if (current.parent && node) edges.add(`${current.parent.name}@${current.parent.version}->${node.name}@${node.version}`)
    for (const section of ['dependencies', 'optionalDependencies']) {
      for (const [childName, child] of Object.entries(pkg[section] ?? {})) {
        stack.push({ package: child, nameHint: childName, parent: node })
      }
    }
  }
  return { nodes: [...nodesByPath.values()], edges: [...edges].sort() }
}

function moduleSpecifiers(filePath, sourceText) {
  const source = ts.createSourceFile(filePath, sourceText, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS)
  const imports = []
  const dynamicOrComputed = []
  if (source.parseDiagnostics.length > 0) dynamicOrComputed.push('runtime module has syntax errors in the static parser')
  const createRequireNames = new Set(['createRequire'])
  function findCreateRequireImports(node) {
    if (ts.isImportDeclaration(node)
      && ts.isStringLiteralLike(node.moduleSpecifier)
      && ['node:module', 'module'].includes(node.moduleSpecifier.text)
      && node.importClause?.namedBindings
      && ts.isNamedImports(node.importClause.namedBindings)) {
      for (const element of node.importClause.namedBindings.elements) {
        if (element.propertyName?.text === 'createRequire' || element.name.text === 'createRequire') createRequireNames.add(element.name.text)
      }
    }
    ts.forEachChild(node, findCreateRequireImports)
  }
  findCreateRequireImports(source)
  function visit(node) {
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
      if (node.moduleSpecifier && ts.isStringLiteralLike(node.moduleSpecifier)) imports.push({ specifier: node.moduleSpecifier.text, kind: 'import' })
      else if (node.moduleSpecifier) dynamicOrComputed.push('non-literal import/export specifier')
    } else if (ts.isCallExpression(node)) {
      if (node.expression.kind === ts.SyntaxKind.ImportKeyword) {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteralLike(argument)) imports.push({ specifier: argument.text, kind: 'import' })
        else dynamicOrComputed.push('non-literal import()')
      } else if (ts.isIdentifier(node.expression) && node.expression.text === 'require') {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteralLike(argument)) imports.push({ specifier: argument.text, kind: 'require' })
        else dynamicOrComputed.push('non-literal require()')
      } else if (ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'require') {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteralLike(argument)) imports.push({ specifier: argument.text, kind: 'require' })
        else dynamicOrComputed.push('non-literal property require()')
      } else if (ts.isIdentifier(node.expression) && createRequireNames.has(node.expression.text)) {
        dynamicOrComputed.push('createRequire usage is not statically traversable')
      } else if (ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'createRequire') {
        dynamicOrComputed.push('createRequire usage is not statically traversable')
      } else if (ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'resolve'
        && ts.isIdentifier(node.expression.expression) && node.expression.expression.text === 'require') {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteralLike(argument)) imports.push({ specifier: argument.text, kind: 'require', traverse: false })
        else dynamicOrComputed.push('non-literal require.resolve()')
      } else if (ts.isPropertyAccessExpression(node.expression) && node.expression.name.text === 'resolve'
        && ts.isMetaProperty(node.expression.expression)
        && node.expression.expression.keywordToken === ts.SyntaxKind.ImportKeyword) {
        const argument = node.arguments[0]
        if (argument && ts.isStringLiteralLike(argument)) imports.push({ specifier: argument.text, kind: 'import', traverse: false })
        else dynamicOrComputed.push('non-literal import.meta.resolve()')
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return { imports, dynamicOrComputed }
}

function runtimePackageForFile(filePath) {
  const relative = path.relative(runtimeModulesDirectory, filePath)
  if (relative.startsWith('..') || path.isAbsolute(relative)) return null
  const parts = relative.split(path.sep)
  const nestedIndex = parts.lastIndexOf('node_modules')
  const packageParts = parts.slice(nestedIndex + 1)
  if (packageParts[0]?.startsWith('@')) return `${packageParts[0]}/${packageParts[1]}`
  return packageParts[0]
}

function packageNameAndSubpath(specifier) {
  const parts = specifier.split('/')
  const scoped = specifier.startsWith('@')
  const packagePartCount = scoped ? 2 : 1
  return {
    name: parts.slice(0, packagePartCount).join('/'),
    subpath: parts.length > packagePartCount ? `./${parts.slice(packagePartCount).join('/')}` : '.',
  }
}

async function existsFile(filePath) {
  try {
    return (await stat(filePath)).isFile()
  } catch {
    return false
  }
}

async function findPackageRoot(fromFile, packageName) {
  let directory = path.dirname(fromFile)
  while (true) {
    const candidate = path.join(directory, 'node_modules', ...packageName.split('/'))
    try {
      if ((await stat(path.join(candidate, 'package.json'))).isFile()) return candidate
    } catch {}
    const parent = path.dirname(directory)
    if (parent === directory) return null
    directory = parent
  }
}

function selectConditionalTarget(value, conditions) {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) {
    for (const candidate of value) {
      const selected = selectConditionalTarget(candidate, conditions)
      if (selected) return selected
    }
    return null
  }
  if (!value || typeof value !== 'object') return null
  for (const [condition, candidate] of Object.entries(value)) {
    if (conditions.has(condition)) {
      const selected = selectConditionalTarget(candidate, conditions)
      if (selected) return selected
    }
  }
  return null
}

function selectPackageTarget(mapping, subpath, conditions) {
  if (typeof mapping === 'string' || Array.isArray(mapping)) return subpath === '.' ? selectConditionalTarget(mapping, conditions) : null
  if (!mapping || typeof mapping !== 'object') return null
  const subpathKeys = Object.keys(mapping).filter(key => key.startsWith('.') || key.startsWith('#'))
  if (subpathKeys.length === 0) return subpath === '.' ? selectConditionalTarget(mapping, conditions) : null
  if (Object.hasOwn(mapping, subpath)) return selectConditionalTarget(mapping[subpath], conditions)
  const patterns = subpathKeys.filter(key => key.includes('*')).sort((left, right) => {
    const leftSpecificity = left.replace('*', '').length
    const rightSpecificity = right.replace('*', '').length
    return rightSpecificity - leftSpecificity
  })
  for (const pattern of patterns) {
    const [prefix, suffix] = pattern.split('*')
    if (!subpath.startsWith(prefix) || !subpath.endsWith(suffix)) continue
    const replacement = subpath.slice(prefix.length, subpath.length - suffix.length)
    const selected = selectConditionalTarget(mapping[pattern], conditions)
    if (selected) return selected.replaceAll('*', replacement)
  }
  return null
}

async function resolveFile(target, kind, packageRoot = null, packageManifest = null) {
  const requestedPath = path.resolve(target)
  if (packageRoot) {
    const relative = path.relative(packageRoot, requestedPath)
    if (relative.startsWith('..') || path.isAbsolute(relative)) return null
  }
  if (await existsFile(requestedPath)) return requestedPath
  if (kind === 'require') {
    for (const extension of ['.js', '.json', '.node', '.cjs', '.mjs']) {
      if (await existsFile(`${requestedPath}${extension}`)) return `${requestedPath}${extension}`
    }
    try {
      if ((await stat(requestedPath)).isDirectory()) {
        try {
          const manifest = JSON.parse(await readFile(path.join(requestedPath, 'package.json'), 'utf8'))
          if (manifest.main) {
            const main = await resolveFile(path.resolve(requestedPath, manifest.main), kind, packageRoot, packageManifest)
            if (main) return main
          }
        } catch {}
        for (const index of ['index.js', 'index.json', 'index.node']) {
          const candidate = path.join(requestedPath, index)
          if (await existsFile(candidate)) return candidate
        }
      }
    } catch {}
  }
  if (requestedPath === packageRoot && packageManifest) {
    for (const entry of [packageManifest.module, packageManifest.main, 'index.mjs', 'index.js', 'index.cjs']) {
      if (!entry) continue
      const candidate = path.resolve(packageRoot, entry)
      if (await existsFile(candidate)) return candidate
    }
  }
  return null
}

async function resolveRuntimeSpecifier(importer, specifier, kind) {
  if (specifier.startsWith('node:') || builtinModules.includes(specifier)) return { builtin: true }
  if (specifier.startsWith('file:')) return { filePath: fileURLToPath(specifier) }
  if (specifier.startsWith('data:')) throw new Error('data URL module imports cannot be traversed statically')
  if (specifier.startsWith('./') || specifier.startsWith('../') || specifier.startsWith('/')) {
    const cleanSpecifier = specifier.split(/[?#]/, 1)[0]
    const candidate = path.resolve(path.dirname(importer), cleanSpecifier)
    const filePath = await resolveFile(candidate, kind)
    if (filePath) return { filePath }
    throw new Error('relative runtime module does not resolve to a file')
  }

  if (specifier.startsWith('#')) {
    let packageDirectory = path.dirname(importer)
    let manifest = null
    while (true) {
      try {
        manifest = JSON.parse(await readFile(path.join(packageDirectory, 'package.json'), 'utf8'))
        break
      } catch {}
      const parent = path.dirname(packageDirectory)
      if (parent === packageDirectory) break
      packageDirectory = parent
    }
    const conditions = new Set(['node-addons', 'node', kind === 'require' ? 'require' : 'import', 'default'])
    const target = selectPackageTarget(manifest?.imports, specifier, conditions)
    if (!target?.startsWith('./')) throw new Error('package import map has no supported target')
    const filePath = await resolveFile(path.resolve(packageDirectory, target), kind, packageDirectory, manifest)
    if (filePath) return { filePath }
    throw new Error('package import map target is missing from the artifact')
  }

  const { name, subpath } = packageNameAndSubpath(specifier)
  const packageDirectory = await findPackageRoot(importer, name)
  if (!packageDirectory) throw new Error(`package ${name} is absent from module resolution paths`)
  const manifest = JSON.parse(await readFile(path.join(packageDirectory, 'package.json'), 'utf8'))
  const conditions = new Set(['node-addons', 'node', kind === 'require' ? 'require' : 'import', 'default'])
  let target
  if (manifest.exports !== undefined) {
    target = selectPackageTarget(manifest.exports, subpath, conditions)
    if (target === null) throw new Error(`package exports do not expose ${specifier}`)
  } else if (subpath !== '.') {
    target = subpath
  } else {
    target = packageDirectory
  }
  if (target === null) throw new Error(`package ${name} has no target for ${specifier}`)
  const targetPath = target.startsWith('./') ? path.resolve(packageDirectory, target) : target === packageDirectory ? packageDirectory : null
  if (!targetPath) throw new Error(`package ${name} exports an unsupported target for ${specifier}`)
  const filePath = await resolveFile(targetPath, kind, packageDirectory, manifest)
  if (filePath) return { filePath }
  throw new Error(`package target for ${specifier} is missing from the artifact`)
}

async function scanRuntimeImports(entryPath, packageByName) {
  const pending = [entryPath]
  const visited = new Set()
  const edges = new Map()
  const unresolved = []
  const nonLiteral = []
  while (pending.length > 0) {
    const modulePath = path.resolve(pending.pop())
    if (visited.has(modulePath)) continue
    visited.add(modulePath)
    const extension = path.extname(modulePath).toLowerCase()
    if (!['.js', '.mjs', '.cjs'].includes(extension)) continue
    let sourceText
    try {
      sourceText = await readFile(modulePath, 'utf8')
    } catch (error) {
      unresolved.push({ from: modulePath, specifier: '', reason: error.message })
      continue
    }
    const parsed = moduleSpecifiers(modulePath, sourceText)
    for (const reason of parsed.dynamicOrComputed) nonLiteral.push({ from: modulePath, reason })
    for (const imported of parsed.imports) {
      let resolved
      try {
        const resolution = await resolveRuntimeSpecifier(modulePath, imported.specifier, imported.kind)
        if (resolution.builtin) continue
        resolved = await realpath(path.resolve(resolution.filePath))
      } catch (error) {
        unresolved.push({ from: modulePath, specifier: imported.specifier, reason: error.message })
        continue
      }
      const relativeToOutput = path.relative(outputDirectory, resolved)
      if (relativeToOutput.startsWith('..') || path.isAbsolute(relativeToOutput)) {
        unresolved.push({ from: modulePath, specifier: imported.specifier, reason: `resolved outside the production artifact: ${resolved}` })
        continue
      }
      const targetPackageName = runtimePackageForFile(resolved)
      const sourcePackageName = runtimePackageForFile(modulePath)
      if (targetPackageName && !packageByName.has(targetPackageName)) {
        unresolved.push({ from: modulePath, specifier: imported.specifier, reason: `resolved package is absent from Nitro manifest: ${targetPackageName}` })
        continue
      }
      if (targetPackageName) {
        const target = packageByName.get(targetPackageName)
        const source = sourcePackageName ? packageByName.get(sourcePackageName) : null
        if (source && source.name !== target.name) {
          const edge = {
            from: `${source.name}@${source.version}`,
            to: `${target.name}@${target.version}`,
            specifier: imported.specifier,
          }
          edges.set(`${edge.from}->${edge.to}`, edge)
        }
      }
      if (imported.traverse !== false && ['.js', '.mjs', '.cjs'].includes(path.extname(resolved).toLowerCase())) pending.push(resolved)
    }
  }
  return {
    moduleCount: visited.size,
    edges: [...edges.values()].sort((left, right) => `${left.from}->${left.to}`.localeCompare(`${right.from}->${right.to}`)),
    unresolved,
    nonLiteral,
  }
}

function validatePeerContext(packageRecord, lockRecords, lockSnapshots, productionNodes, runtimePackages, importGraph) {
  const { name, version, manifest } = packageRecord
  const key = `${name}@${version}`
  const lockPackage = lockRecords.get(key)?.[0]?.packageMetadata
  if (!lockPackage) fail(`lock package metadata is missing for ${key}`)
  const peerDependencies = manifest.peerDependencies ?? {}
  const peerMetadata = manifest.peerDependenciesMeta ?? {}
  const absentOptionalPeers = []
  for (const [peerName, peerRange] of Object.entries(peerDependencies)) {
    const optional = peerMetadata[peerName]?.optional === true
    const lockedMetadata = lockPackage.peerDependenciesMeta?.[peerName]?.optional === true
    if (optional !== lockedMetadata) fail(`peer optionality differs between artifact and lock for ${key} -> ${peerName}`)
    if (lockPackage.peerDependencies?.[peerName] !== peerRange) fail(`peer range differs between artifact and lock for ${key} -> ${peerName}`)
    const runtimePeer = runtimePackages.get(peerName)
    if (runtimePeer) continue
    if (!optional) fail(`required peer ${peerName} is absent from artifact package ${key}`)
    const snapshots = lockSnapshots.get(key) ?? []
    const providers = snapshots.map(item => ({ key: item.key, version: item.snapshot?.optionalDependencies?.[peerName] ?? null }))
    const presentProvider = providers.find(provider => typeof provider.version === 'string')
    if (!presentProvider) fail(`optional peer ${peerName} has no lockfile context for ${key}`)
    const sourceNode = productionNodes.find(node => node.name === name && node.version === version && node.virtualStoreKey)
    const providerVersion = presentProvider.version.split('(')[0]
    const encodedProvider = `${peerName.replaceAll('/', '+')}@${providerVersion}`
    if (!sourceNode?.virtualStoreKey?.includes(encodedProvider)) {
      fail(`frozen install peer context for ${key} does not bind ${peerName}@${providerVersion}`)
    }
    if (importGraph.edges.some(edge => edge.from === key && edge.to.startsWith(`${peerName}@`))) {
      fail(`runtime graph imports absent optional peer ${peerName} from ${key}`)
    }
    absentOptionalPeers.push({
      name: peerName,
      range: peerRange,
      version: providerVersion,
      lockSnapshot: presentProvider.key,
      workspaceStoreKey: sourceNode.virtualStoreKey,
      absentFromArtifact: true,
      noRuntimeImport: true,
    })
  }
  return absentOptionalPeers
}

async function proveBundledPackages(bundleProvenance, productionNodes, packageRecords, snapshots) {
  const packageEvidence = []
  for (const bundledPackage of bundleProvenance.packages) {
    const key = `${bundledPackage.name}@${bundledPackage.version}`
    const candidate = productionNodes.find(node => node.name === bundledPackage.name
      && node.version === bundledPackage.version
      && node.virtualStoreKey === bundledPackage.sourceVirtualStoreKey
      && node.path)
    if (!candidate) fail(`bundled package source is absent from the frozen production graph: ${key} (${bundledPackage.sourceVirtualStoreKey})`)

    const sourcePath = await realpath(candidate.path)
    const sourceManifest = JSON.parse(await readFile(path.join(sourcePath, 'package.json'), 'utf8'))
    const sourceMetadata = {
      name: sourceManifest.name,
      version: sourceManifest.version,
      dependencies: sourceManifest.dependencies ?? {},
      optionalDependencies: sourceManifest.optionalDependencies ?? {},
      peerDependencies: sourceManifest.peerDependencies ?? {},
      peerDependenciesMeta: sourceManifest.peerDependenciesMeta ?? {},
    }
    if (sourceMetadata.name !== bundledPackage.name || sourceMetadata.version !== bundledPackage.version) {
      fail(`bundled package manifest identity differs from its frozen production graph node: ${key}`)
    }

    const lockedRecords = packageRecords.get(key) ?? []
    const integrityValues = [...new Set(lockedRecords.map(record => record.integrity).filter(Boolean))]
    if (integrityValues.length !== 1) fail(`expected one frozen integrity for bundled ${key}; found ${integrityValues.length}`)
    const sourceFileProofs = []
    for (const module of bundledPackage.modules) {
      const sourceFilePath = path.resolve(sourcePath, module.sourceFile)
      const relativeSourceFile = path.relative(sourcePath, sourceFilePath)
      if (relativeSourceFile.startsWith('..') || path.isAbsolute(relativeSourceFile)) {
        fail(`bundled module source escapes its frozen package: ${key}/${module.sourceFile}`)
      }
      const resolvedSourceFile = await realpath(sourceFilePath)
      const resolvedRelativeSource = path.relative(sourcePath, resolvedSourceFile)
      if (resolvedRelativeSource.startsWith('..') || path.isAbsolute(resolvedRelativeSource)) {
        fail(`bundled module source resolves outside its frozen package: ${key}/${module.sourceFile}`)
      }
      sourceFileProofs.push({
        ...module,
        sha256: sha256(await readFile(resolvedSourceFile)),
      })
    }

    const lockedSnapshots = snapshots.get(key) ?? []
    if (lockedSnapshots.length === 0) fail(`frozen lock snapshot is missing for bundled ${key}`)
    packageEvidence.push({
      ...bundledPackage,
      packageManifestSha256: sha256(stable(sourceMetadata)),
      sourceFileProofs,
      lock: {
        integrity: integrityValues[0],
        packageRecords: lockedRecords.map(record => record.key),
        snapshotKeys: lockedSnapshots.map(snapshot => snapshot.key),
      },
      lockVerified: true,
    })
  }
  return packageEvidence
}

async function prepare() {
  const topManifest = JSON.parse(await readFile(packagePath, 'utf8'))
  const yamlVersion = topManifest.devDependencies?.yaml
  if (!/^\d+\.\d+\.\d+$/.test(yamlVersion ?? '')) fail('root yaml tooling version is not pinned')
  parseAllDocuments = loadWorkspacePackage('yaml', yamlVersion, 'dist/index.js').parseAllDocuments
  const serverManifestPath = path.join(serverDirectory, 'package.json')
  const serverManifest = JSON.parse(await readFile(serverManifestPath, 'utf8'))
  if (serverManifest.packageManager) fail('Nuxt runtime output declares a package manager; the standalone contract requires direct Node startup')
  for (const lockfile of ['pnpm-lock.yaml', 'package-lock.json', 'npm-shrinkwrap.json', 'yarn.lock']) {
    try {
      await stat(path.join(serverDirectory, lockfile))
      fail(`Nuxt runtime output unexpectedly contains ${lockfile}`)
    } catch (error) {
      if (error.code !== 'ENOENT') throw error
    }
  }
  const lockText = await readFile(lockPath, 'utf8')
  const { project: lock, packageRecords, snapshots } = readProjectLock(lockText)
  const lockedTypeScript = projectDependencyVersion(lock.importers['apps/web-console'], 'devDependencies', 'typescript')
  ts = loadWorkspacePackage('typescript', lockedTypeScript, 'lib/typescript.js')
  const runtimePackages = await readArtifactPackages(serverManifest)
  const runtimeByName = new Map(runtimePackages.map(pkg => [pkg.name, pkg]))

  const listResult = spawnSync('corepack', ['pnpm', '--filter', '@pulsegrid/web-console', 'list', '--prod', '--depth', 'Infinity', '--json'], {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 100 * 1024 * 1024,
  })
  if (listResult.error || listResult.status !== 0) {
    fail(`unable to inspect frozen production graph with pnpm list: ${listResult.error?.message ?? listResult.stderr}`)
  }
  const graph = flattenProductionGraph(JSON.parse(listResult.stdout)[0])
  const graphEdges = new Set(graph.edges)
  const bundleProvenance = await collectRuntimeBundleProvenance(outputDirectory)
  const bundledPackageEvidence = await proveBundledPackages(bundleProvenance, graph.nodes, packageRecords, snapshots)
  const packageEvidence = []

  for (const runtimePackage of runtimePackages) {
    const key = `${runtimePackage.name}@${runtimePackage.version}`
    const lockedRecords = packageRecords.get(key) ?? []
    const integrityValues = [...new Set(lockedRecords.map(record => record.integrity).filter(Boolean))]
    if (integrityValues.length !== 1) fail(`expected one frozen integrity for ${key}; found ${integrityValues.length}`)
    const productionCandidates = graph.nodes.filter(node => node.name === runtimePackage.name && node.version === runtimePackage.version && node.path)
    if (productionCandidates.length === 0) fail(`runtime package is absent from frozen production graph: ${key}`)

    let matchingCandidate = null
    let fileProof = null
    for (const candidate of productionCandidates) {
      const sourcePath = await realpath(candidate.path)
      const sourceManifest = JSON.parse(await readFile(path.join(sourcePath, 'package.json'), 'utf8'))
      const artifactMetadata = runtimePackage.manifestMetadata
      const sourceMetadata = {
        name: sourceManifest.name,
        version: sourceManifest.version,
        dependencies: sourceManifest.dependencies ?? {},
        optionalDependencies: sourceManifest.optionalDependencies ?? {},
        peerDependencies: sourceManifest.peerDependencies ?? {},
        peerDependenciesMeta: sourceManifest.peerDependenciesMeta ?? {},
      }
      const candidateProof = await packageFileProof(runtimePackage.directory, sourcePath)
      if (candidateProof && stable(sourceMetadata) === stable(artifactMetadata)) {
        matchingCandidate = { ...candidate, path: sourcePath }
        fileProof = candidateProof
        break
      }
    }
    if (!matchingCandidate || !fileProof) fail(`artifact files or package dependency/peer metadata do not match a frozen production package: ${key}`)
    const lockedSnapshots = snapshots.get(key) ?? []
    if (lockedSnapshots.length === 0) fail(`frozen lock snapshot is missing for ${key}`)
    packageEvidence.push({
      name: runtimePackage.name,
      version: runtimePackage.version,
      packageManifestSha256: sha256(stable(runtimePackage.manifestMetadata)),
      artifactTree: await treeDigest(runtimePackage.directory),
      sourcePackagePath: path.relative(root, matchingCandidate.path),
      sourceVirtualStoreKey: matchingCandidate.virtualStoreKey,
      sourceNonManifestFileProof: fileProof,
      lock: {
        integrity: integrityValues[0],
        packageRecords: lockedRecords.map(record => record.key),
        snapshotKeys: lockedSnapshots.map(snapshot => snapshot.key),
      },
    })
  }

  const importGraph = await scanRuntimeImports(path.join(serverDirectory, 'index.mjs'), runtimeByName)
  if (importGraph.unresolved.length > 0) fail(`runtime graph has ${importGraph.unresolved.length} unresolved or out-of-artifact import(s): ${JSON.stringify(importGraph.unresolved.slice(0, 10))}`)
  if (importGraph.nonLiteral.length > 0) fail(`runtime graph has ${importGraph.nonLiteral.length} non-literal import(s): ${JSON.stringify(importGraph.nonLiteral.slice(0, 10))}`)
  for (const edge of importGraph.edges) {
    if (!graphEdges.has(`${edge.from}->${edge.to}`)) fail(`emitted runtime edge is absent from the frozen production graph: ${edge.from} -> ${edge.to}`)
  }

  const optionalPeers = []
  for (const runtimePackage of runtimePackages) {
    const contexts = validatePeerContext(runtimePackage, packageRecords, snapshots, graph.nodes, runtimeByName, importGraph)
    if (contexts.length > 0) optionalPeers.push({ package: `${runtimePackage.name}@${runtimePackage.version}`, contexts })
  }
  for (const name of ['unhead', 'vue']) {
    if (!runtimeByName.has(name)) fail(`previously verified ${name} runtime package is missing; dependency graph requires re-plan`)
    const packageEntry = optionalPeers.find(item => item.package === `${name}@${runtimeByName.get(name).version}`)
    const expectedPeer = name === 'unhead' ? 'vite' : 'typescript'
    if (!packageEntry?.contexts.some(context => context.name === expectedPeer)) {
      fail(`previously verified ${name}/${expectedPeer} peer context is missing; dependency graph requires re-plan`)
    }
  }

  const outputProof = await treeDigest(outputDirectory)
  const pnpmVersionResult = spawnSync('corepack', ['pnpm', '--version'], { cwd: root, encoding: 'utf8' })
  if (pnpmVersionResult.status !== 0) fail('unable to read the build pnpm version')
  const pnpmVersion = pnpmVersionResult.stdout.trim()
  if (topManifest.packageManager !== `pnpm@${pnpmVersion}`) fail('active pnpm version does not match package.json packageManager')
  const nodeVersion = process.versions.node
  if (nodeVersion !== (await readFile(path.join(root, '.node-version'), 'utf8')).trim()) fail('active Node version does not match .node-version')

  const evidence = {
    schemaVersion: 2,
    source: {
      commit: process.env.GITHUB_SHA ?? spawnSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).stdout.trim(),
      runId: process.env.GITHUB_RUN_ID ?? null,
      runAttempt: process.env.GITHUB_RUN_ATTEMPT ?? null,
      lockSha256: sha256(lockText),
      nodeVersion,
      pnpmVersion,
      buildCommand: 'corepack pnpm run web:build',
      productionImporter: 'apps/web-console',
      lockfileVersion: lock.lockfileVersion,
    },
    artifact: {
      path: 'apps/web-console/.output',
      sha256: outputProof.digest,
      fileCount: outputProof.fileCount,
      runtimeManifestSha256: sha256(await readFile(serverManifestPath)),
      packages: packageEvidence,
      bundleProvenance: {
        ...bundleProvenance,
        packages: bundledPackageEvidence,
      },
      graph: {
        entry: 'apps/web-console/.output/server/index.mjs',
        moduleCount: importGraph.moduleCount,
        packageEdges: importGraph.edges.map(edge => ({ ...edge, lockVerified: true })),
        unresolved: importGraph.unresolved,
        nonLiteral: importGraph.nonLiteral,
        sourceGraphNodeCount: graph.nodes.length,
        sourceGraphEdgeCount: graphEdges.size,
        allEdgesInFrozenProductionGraph: true,
      },
      optionalPeerContexts: optionalPeers,
    },
  }
  await writeFile(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`, { flag: 'w' })
  console.log(`Prepared runtime evidence for .output (${packageEvidence.length} physical packages, ${bundledPackageEvidence.length} bundled package contexts across ${bundleProvenance.moduleCount} modules, ${importGraph.edges.length} physical package edges).`)
  console.log(`Artifact SHA-256: ${outputProof.digest}`)
}

function projectDependencyVersion(importer, section, name) {
  const version = importer?.[section]?.[name]?.version
  if (typeof version !== 'string') fail(`frozen lock resolution is missing ${section}.${name}`)
  return version.split('(')[0]
}

await prepare().catch((error) => {
  console.error(`unable to prepare runtime artifact evidence: ${error.message}`)
  process.exitCode = 1
})
