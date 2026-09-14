import { describe, expect, it } from 'vitest'

import { parseAppEnvironment, parseBackendOrigin } from '../../shared/app-environment'

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

describe('parseBackendOrigin', () => {
  it('accepts the environment-specific loopback origin', () => {
    expect(parseBackendOrigin('http://127.0.0.1:8080', 'development')).toBe('http://127.0.0.1:8080')
    expect(parseBackendOrigin('http://127.0.0.1:18080/', 'test')).toBe('http://127.0.0.1:18080')
  })

  it.each([
    '',
    'http://localhost:8080',
    'https://127.0.0.1:8080',
    'http://127.0.0.1:8080/path',
    'http://user:pass@127.0.0.1:8080',
    'http://127.0.0.1:18080',
  ])('rejects unsafe or mismatched origin %s', (value) => {
    expect(() => parseBackendOrigin(value, 'development')).toThrow('NUXT_BACKEND_ORIGIN')
  })

  it('requires the isolated test origin and keeps production origin private', () => {
    expect(() => parseBackendOrigin('http://127.0.0.1:8080', 'test')).toThrow('18080')
    expect(parseBackendOrigin('', 'production')).toBeUndefined()
    expect(() => parseBackendOrigin('http://127.0.0.1:8080', 'production')).toThrow('not supported')
  })
})
