import { spawn } from 'node:child_process'
import process from 'node:process'

if (process.env.NUXT_BACKEND_ORIGIN !== 'http://127.0.0.1:18080') {
  console.error('NUXT_BACKEND_ORIGIN must be http://127.0.0.1:18080 for browser tests')
  process.exit(1)
}

const server = spawn(process.execPath, ['apps/web-console/.output/server/index.mjs'], {
  env: {
    ...process.env,
    NUXT_APP_ENV: 'test',
    NUXT_BACKEND_ORIGIN: 'http://127.0.0.1:18080',
    NITRO_HOST: '127.0.0.1',
    NITRO_PORT: '4173',
  },
  stdio: 'inherit',
})

let shuttingDown = false
function stop(signal) {
  if (shuttingDown) return
  shuttingDown = true
  server.kill(signal)
}

process.on('SIGINT', () => stop('SIGINT'))
process.on('SIGTERM', () => stop('SIGTERM'))

server.on('error', (error) => {
  console.error(`unable to start Nuxt test server: ${error.message}`)
  process.exit(1)
})

server.on('exit', (code, signal) => {
  if (signal && !shuttingDown) process.exit(1)
  process.exit(code ?? 0)
})
