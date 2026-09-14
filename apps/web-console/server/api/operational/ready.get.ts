import { setResponseHeader, setResponseStatus } from 'h3'

import { fetchBackendReadiness } from '../../utils/backend-readiness'

export default defineEventHandler(async (event) => {
  setResponseHeader(event, 'cache-control', 'no-store')
  const runtimeConfig = useRuntimeConfig(event)

  if (typeof runtimeConfig.backendOrigin !== 'string' || runtimeConfig.backendOrigin === '') {
    setResponseStatus(event, 503)
    return { status: 'unavailable' }
  }

  const readiness = await fetchBackendReadiness(runtimeConfig.backendOrigin)
  setResponseStatus(event, readiness.status === 'ready' ? 200 : 503)
  return readiness
})
