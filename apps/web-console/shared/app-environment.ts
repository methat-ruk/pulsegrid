export const APP_ENVIRONMENTS = ['development', 'test', 'production'] as const

export type AppEnvironment = (typeof APP_ENVIRONMENTS)[number]

export function parseAppEnvironment(value: unknown): AppEnvironment {
  if (typeof value === 'string' && APP_ENVIRONMENTS.includes(value as AppEnvironment)) {
    return value as AppEnvironment
  }

  throw new Error('NUXT_APP_ENV must be one of: development, test, production')
}
