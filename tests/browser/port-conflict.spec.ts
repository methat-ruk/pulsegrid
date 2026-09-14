import { createServer, type Server } from 'node:http'

import { expect, test } from '@playwright/test'

import { assertPortAvailable } from './fixtures'

test('browser API fixture rejects an occupied test port', async () => {
  const blocker: Server = createServer((_request, response) => response.end('blocked'))
  await new Promise<void>(resolve => blocker.listen(18080, '127.0.0.1', () => resolve()))

  try {
    await expect(assertPortAvailable()).rejects.toThrow('already in use')
  }
  finally {
    await new Promise<void>(resolve => blocker.close(() => resolve()))
  }
})
