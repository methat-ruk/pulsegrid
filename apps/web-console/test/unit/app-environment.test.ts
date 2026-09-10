import { describe, expect, it } from 'vitest'

import { parseAppEnvironment } from '../../shared/app-environment'

describe('parseAppEnvironment', () => {
  it.each(['development', 'test', 'production'])('accepts %s', (value) => {
    expect(parseAppEnvironment(value)).toBe(value)
  })

  it.each([undefined, '', 'staging', 'Development', 42])('rejects %s', (value) => {
    expect(() => parseAppEnvironment(value)).toThrow(
      'NUXT_APP_ENV must be one of: development, test, production',
    )
  })
})
