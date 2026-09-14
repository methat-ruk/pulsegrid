import type { BackendReadiness } from '../../shared/backend-readiness'

const BACKEND_READINESS_PATH = '/health/ready'
const READINESS_TIMEOUT_MS = 1_500
const MAX_RESPONSE_BYTES = 1_024

function unavailable(): BackendReadiness {
  return { status: 'unavailable' }
}

async function readBoundedBody(response: Response): Promise<string | undefined> {
  if (response.body === null) return undefined

  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      total += value.byteLength
      if (total > MAX_RESPONSE_BYTES) {
        await reader.cancel()
        return undefined
      }
      chunks.push(value)
    }
  }
  finally {
    reader.releaseLock()
  }

  const body = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    body.set(chunk, offset)
    offset += chunk.byteLength
  }
  return new TextDecoder().decode(body)
}

export async function fetchBackendReadiness(backendOrigin: string): Promise<BackendReadiness> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), READINESS_TIMEOUT_MS)

  try {
    const response = await fetch(`${backendOrigin}${BACKEND_READINESS_PATH}`, {
      headers: { accept: 'application/json' },
      redirect: 'manual',
      signal: controller.signal,
    })

    if (response.status !== 200) return unavailable()

    const contentLength = response.headers.get('content-length')
    if (contentLength !== null && Number(contentLength) > MAX_RESPONSE_BYTES) {
      return unavailable()
    }

    const body = await readBoundedBody(response)
    if (body === undefined) return unavailable()

    try {
      const parsed: unknown = JSON.parse(body)
      if (
        typeof parsed === 'object'
        && parsed !== null
        && Object.keys(parsed).length === 1
        && 'status' in parsed
        && parsed.status === 'ready'
      ) {
        return { status: 'ready' }
      }
    }
    catch {
      // Invalid upstream JSON is intentionally reported as unavailable.
    }

    return unavailable()
  }
  catch {
    return unavailable()
  }
  finally {
    clearTimeout(timeout)
  }
}
