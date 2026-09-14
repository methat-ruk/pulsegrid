export const APP_ENVIRONMENTS = ['development', 'test', 'production'] as const

export type AppEnvironment = (typeof APP_ENVIRONMENTS)[number]

const backendOriginError = 'NUXT_BACKEND_ORIGIN must be an HTTP loopback origin with no path, credentials, query, or fragment'

export function parseBackendOrigin(value: unknown, environment: AppEnvironment): string | undefined {
  if (typeof value !== 'string' || value.trim() === '') {
    if (environment === 'production') return undefined
    throw new Error(`${backendOriginError}; it is required in ${environment}`)
  }

  if (environment === 'production') {
    throw new Error('NUXT_BACKEND_ORIGIN is not supported in production')
  }

  let origin: URL
  try {
    origin = new URL(value)
  }
  catch {
    throw new Error(backendOriginError)
  }

  const expectedPort = environment === 'test' ? '18080' : '8080'
  if (
    origin.protocol !== 'http:'
    || origin.hostname !== '127.0.0.1'
    || origin.port !== expectedPort
    || origin.username !== ''
    || origin.password !== ''
    || origin.pathname !== '/'
    || origin.search !== ''
    || origin.hash !== ''
  ) {
    throw new Error(`${backendOriginError}; expected http://127.0.0.1:${expectedPort}`)
  }

  return origin.origin
}

export function parseAppEnvironment(value: unknown): AppEnvironment {
  if (typeof value === 'string' && APP_ENVIRONMENTS.includes(value as AppEnvironment)) {
    return value as AppEnvironment
  }

  throw new Error('NUXT_APP_ENV must be one of: development, test, production')
}
