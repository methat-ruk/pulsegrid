<script setup lang="ts">
const error = useError()

const pageTitle = computed(() => (
  error.value?.status === 404
    ? 'Page not found · PulseGrid Console'
    : 'Error · PulseGrid Console'
))

useHead({ title: pageTitle })

const referenceCode = computed(() => {
  const code = error.value?.status
  return typeof code === 'number' && code >= 400 && code < 600 ? code : 500
})

const clearCurrentError = async () => {
  await clearError({ redirect: '/' })
}
</script>

<template>
  <div class="error-page">
    <div class="error-page-content">
      <p class="text-sm font-semibold uppercase tracking-label text-pulse-text-muted">
        PulseGrid Console
      </p>
      <h1 class="error-page-title">
        Something needs attention.
      </h1>
      <p class="error-page-description">
        The console could not complete this request. Return to the overview and
        try again.
      </p>
      <p class="text-sm text-pulse-text-muted">
        Reference: {{ referenceCode }}
      </p>
      <button
        class="error-page-action"
        type="button"
        @click="clearCurrentError"
      >
        Return to overview
      </button>
    </div>
  </div>
</template>
