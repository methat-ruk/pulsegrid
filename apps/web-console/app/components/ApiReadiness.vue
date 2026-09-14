<script setup lang="ts">
import type { BackendReadiness } from '../../shared/backend-readiness'

type ReadinessState = 'checking' | 'ready' | 'unavailable'

const state = ref<ReadinessState>('checking')
let requestId = 0
let requestController: AbortController | undefined

const checkReadiness = async () => {
  const currentRequestId = ++requestId
  requestController?.abort()
  requestController = new AbortController()
  state.value = 'checking'

  try {
    const result = await $fetch<BackendReadiness>('/api/operational/ready', {
      signal: requestController.signal,
    })
    if (currentRequestId === requestId) {
      state.value = result.status === 'ready' ? 'ready' : 'unavailable'
    }
  }
  catch {
    if (currentRequestId === requestId) state.value = 'unavailable'
  }
}

onMounted(checkReadiness)
onBeforeUnmount(() => {
  requestId += 1
  requestController?.abort()
})
</script>

<template>
  <section
    class="api-readiness"
    aria-labelledby="api-readiness-title"
  >
    <div class="api-readiness-content">
      <div
        class="api-readiness-icon"
        aria-hidden="true"
      >
        <UIcon
          :name="state === 'ready' ? 'i-lucide-circle-check' : 'i-lucide-circle-alert'"
          class="size-6"
        />
      </div>
      <div>
        <h2
          id="api-readiness-title"
          class="api-readiness-title"
        >
          API connection
        </h2>
        <p
          class="api-readiness-status"
          role="status"
          aria-live="polite"
        >
          <template v-if="state === 'checking'">
            Checking the local API…
          </template>
          <template v-else-if="state === 'ready'">
            Local API is ready.
          </template>
          <template v-else>
            Local API is unavailable.
          </template>
        </p>
        <p class="api-readiness-description">
          This only confirms that the local API process is accepting requests.
        </p>
        <button
          v-if="state === 'unavailable'"
          class="api-readiness-retry"
          type="button"
          @click="checkReadiness"
        >
          Retry connection
        </button>
      </div>
    </div>
  </section>
</template>
