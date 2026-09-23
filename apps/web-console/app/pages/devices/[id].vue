<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import {
  DeviceGraphQLError,
  getDevice,
  type Device,
} from '../../features/devices/device-graphql'

const route = useRoute()
const device = ref<Device | null>(null)
const state = ref<'loading' | 'ready' | 'not-found' | 'error'>('loading')
const errorMessage = ref('')
const pageHeading = ref<HTMLElement | null>(null)
let requestController: AbortController | undefined
let requestSequence = 0

const deviceId = computed(() => {
  const raw = route.params.id
  return Array.isArray(raw) ? raw[0] ?? '' : raw ?? ''
})

useHead(() => ({ title: device.value ? `${device.value.deviceKey} · PulseGrid Console` : 'Device · PulseGrid Console' }))

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? 'Unknown date'
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: 'medium',
        timeStyle: 'short',
      }).format(date)
}

function friendlyError(error: unknown): string {
  if (error instanceof DeviceGraphQLError && error.code === 'SERVICE_UNAVAILABLE') {
    return 'The device service is unavailable. Try again.'
  }
  if (error instanceof DeviceGraphQLError && error.code === 'BAD_USER_INPUT') return error.message
  return 'The device could not be loaded. Try again.'
}

async function loadDevice() {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  state.value = 'loading'
  errorMessage.value = ''
  try {
    const result = await getDevice(deviceId.value, controller.signal)
    if (sequence !== requestSequence) return
    device.value = result.device
    state.value = result.device === null ? 'not-found' : 'ready'
    await nextTick()
    pageHeading.value?.focus()
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== requestSequence) return
    state.value = 'error'
    errorMessage.value = friendlyError(error)
    await nextTick()
    pageHeading.value?.focus()
  }
}

onMounted(() => void loadDevice())
watch(deviceId, () => {
  if (import.meta.client) void loadDevice()
})
onBeforeUnmount(() => {
  requestSequence += 1
  requestController?.abort()
})
</script>

<template>
  <section aria-labelledby="page-title">
    <NuxtLink
      class="back-link"
      to="/devices"
    >
      <UIcon
        name="i-lucide-arrow-left"
        class="size-4"
        aria-hidden="true"
      />
      Back to devices
    </NuxtLink>

    <div
      v-if="state === 'loading'"
      class="state-panel"
      role="status"
      aria-live="polite"
    >
      <UIcon
        name="i-lucide-loader-circle"
        class="size-6 animate-spin"
        aria-hidden="true"
      />
      <span>Loading device…</span>
    </div>

    <div
      v-else-if="state === 'not-found'"
      class="state-panel state-panel--empty"
      role="status"
    >
      <UIcon
        name="i-lucide-search-x"
        class="size-7"
        aria-hidden="true"
      />
      <div>
        <h1
          id="page-title"
          ref="pageHeading"
          tabindex="-1"
        >
          Device not found.
        </h1>
        <p>This device does not exist in the development registry.</p>
        <NuxtLink
          class="primary-action"
          to="/devices"
        >
          Back to devices
        </NuxtLink>
      </div>
    </div>

    <div
      v-else-if="state === 'error'"
      class="state-panel state-panel--error"
      role="alert"
    >
      <UIcon
        name="i-lucide-circle-alert"
        class="size-6"
        aria-hidden="true"
      />
      <div>
        <h1
          id="page-title"
          ref="pageHeading"
          tabindex="-1"
        >
          Device could not be loaded.
        </h1>
        <p>{{ errorMessage }}</p>
        <button
          class="primary-action"
          type="button"
          @click="loadDevice"
        >
          Try again
        </button>
      </div>
    </div>

    <template v-else-if="device">
      <p class="eyebrow">
        Device detail
      </p>
      <h1
        id="page-title"
        ref="pageHeading"
        class="page-heading"
        tabindex="-1"
      >
        {{ device.displayName }}
      </h1>
      <p class="page-intro">
        {{ device.deviceKey }}
      </p>
      <dl class="detail-card">
        <div>
          <dt>Device key</dt>
          <dd>{{ device.deviceKey }}</dd>
        </div>
        <div>
          <dt>Display name</dt>
          <dd>{{ device.displayName }}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>{{ formatDate(device.createdAt) }}</dd>
        </div>
        <div>
          <dt>Identifier</dt>
          <dd class="detail-id">
            {{ device.id }}
          </dd>
        </div>
      </dl>
      <DeviceTelemetryPanel :device-id="device.id" />
    </template>
  </section>
</template>
