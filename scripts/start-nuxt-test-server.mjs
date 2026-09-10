import { spawn } from 'node:child_process'
import process from 'node:process'

const server = spawn(process.execPath, ['apps/web-console/.output/server/index.mjs'], {
  env: {
    ...process.env,
    NUXT_APP_ENV: 'test',
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
