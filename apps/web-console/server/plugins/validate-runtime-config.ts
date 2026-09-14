import { defineNitroPlugin, useRuntimeConfig } from 'nitropack/runtime'

import { parseAppEnvironment, parseBackendOrigin } from '../../shared/app-environment'

export default defineNitroPlugin(() => {
  const runtimeConfig = useRuntimeConfig()
  const appEnvironment = parseAppEnvironment(runtimeConfig.appEnv)
  parseBackendOrigin(runtimeConfig.backendOrigin, appEnvironment)
})
