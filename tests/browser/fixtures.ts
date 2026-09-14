import { once } from 'node:events'
import { spawn, type ChildProcess } from 'node:child_process'
import { access } from 'node:fs/promises'
import { join } from 'node:path'

import { test as base } from '@playwright/test'

const apiURL = 'http://127.0.0.1:18080/health/ready'
const apiBinary = join(process.cwd(), '.output', 'api-test')

export type ApiProcess = {
  start: () => Promise<void>
  stop: () => Promise<void>
}

async function waitForAPI(child: ChildProcess): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (child.exitCode !== null) throw new Error(`API process exited with code ${child.exitCode}`)
    try {
      const response = await fetch(apiURL)
      if (response.status === 200) return
    } catch {
      // The listener may still be starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('API process did not become ready within 5 seconds')
}

async function stopProcess(child: ChildProcess | undefined): Promise<void> {
  if (!child || child.exitCode !== null) return
  const exited = once(child, 'exit')
  child.kill('SIGTERM')
  const graceful = await Promise.race([
    exited.then(() => true),
    new Promise<boolean>(resolve => setTimeout(() => resolve(false), 2_000)),
  ])
  if (!graceful && child.exitCode === null) {
    child.kill('SIGKILL')
    await exited
  }
}

async function createAPIProcess(): Promise<ApiProcess> {
  await access(apiBinary)
  let child: ChildProcess | undefined

  const start = async () => {
    if (child && child.exitCode === null) return
    child = spawn(apiBinary, [], {
      env: {
        ...process.env,
        PULSEGRID_ENV: 'test',
        PULSEGRID_HTTP_HOST: '127.0.0.1',
        PULSEGRID_HTTP_PORT: '18080',
        PULSEGRID_LOG_LEVEL: 'error',
        PULSEGRID_SHUTDOWN_TIMEOUT: '1s',
      },
      stdio: 'ignore',
    })
    await waitForAPI(child)
  }

  const stop = async () => {
    await stopProcess(child)
    child = undefined
  }

  await start()
  return { start, stop }
}

export const test = base.extend<{ apiProcess: ApiProcess }>({
  apiProcess: [async ({}, use) => {
    const api = await createAPIProcess()
    try {
      await use(api)
    } finally {
      await api.stop()
    }
  }, { scope: 'worker', auto: true }],
})

export { expect } from '@playwright/test'
