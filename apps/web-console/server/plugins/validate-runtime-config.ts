import { defineNitroPlugin, useRuntimeConfig } from 'nitropack/runtime'

import { parseAppEnvironment } from '../../shared/app-environment'

export default defineNitroPlugin(() => {
  const runtimeConfig = useRuntimeConfig()
  parseAppEnvironment(runtimeConfig.appEnv)
})
