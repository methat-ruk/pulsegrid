import { proxyGraphqlRequest } from '../utils/graphql-proxy'

export default defineEventHandler(async (event) => {
  const runtimeConfig = useRuntimeConfig(event)
  await proxyGraphqlRequest(event, typeof runtimeConfig.backendOrigin === 'string' ? runtimeConfig.backendOrigin : undefined)
})
