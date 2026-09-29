<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { getAlert, type AlertOccurrence } from '../../features/alerts/alert-graphql'
import { GraphQLClientError } from '../../features/graphql/client'
import { getDevice, type Device } from '../../features/devices/device-graphql'

const route = useRoute()
const alert = ref<AlertOccurrence | null>(null)
const device = ref<Device | null>(null)
const state = ref<'loading' | 'not-found' | 'error' | 'ready'>('loading')
const deviceLabelState = ref<'loading' | 'ready' | 'unavailable'>('loading')
const errorMessage = ref('')
const pageHeading = ref<HTMLElement | null>(null)
let requestController: AbortController | undefined
let requestSequence = 0

const alertId = computed(() => {
  const raw = route.params.id
  return Array.isArray(raw) ? raw[0] ?? '' : raw ?? ''
})
const backToList = computed(() => (
  typeof route.query.deviceId === 'string'
    ? { path: '/alerts', query: { deviceId: route.query.deviceId } }
    : '/alerts'
))

useHead({ title: 'Alert · PulseGrid Console' })

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? 'Unknown date'
    : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatNumber(value: number): string {
  return Number.isFinite(value) ? String(value) : 'Unknown'
}

function comparisonText(value: AlertOccurrence): string {
  const comparison = {
    GT: 'is greater than',
    GTE: 'is greater than or equal to',
    LT: 'is less than',
    LTE: 'is less than or equal to',
  }[value.comparator]
  return `Temperature: ${formatNumber(value.temperatureCelsius)} °C ${comparison} ${formatNumber(value.thresholdCelsius)} °C`
}

function devicePath(id: string): string {
  return `/devices/${encodeURIComponent(id)}`
}

async function loadAlert() {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  alert.value = null
  device.value = null
  deviceLabelState.value = 'loading'
  state.value = 'loading'
  errorMessage.value = ''

  try {
    const result = await getAlert(alertId.value, controller.signal)
    if (sequence !== requestSequence) return
    if (result.alert === null) {
      state.value = 'not-found'
      await nextTick()
      pageHeading.value?.focus()
      return
    }

    alert.value = result.alert
    state.value = 'ready'
    await nextTick()
    pageHeading.value?.focus()

    try {
      const deviceResult = await getDevice(result.alert.deviceId, controller.signal)
      if (sequence !== requestSequence) return
      device.value = deviceResult.device
      deviceLabelState.value = deviceResult.device === null ? 'unavailable' : 'ready'
    }
    catch {
      if (controller.signal.aborted || sequence !== requestSequence) return
      deviceLabelState.value = 'unavailable'
    }
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== requestSequence) return
    state.value = 'error'
    errorMessage.value = error instanceof GraphQLClientError && error.code === 'SERVICE_UNAVAILABLE'
      ? 'The alert service is unavailable. Try again.'
      : 'This alert could not be loaded. Check the link and try again.'
    deviceLabelState.value = 'unavailable'
    await nextTick()
    pageHeading.value?.focus()
  }
  finally {
    if (sequence === requestSequence) requestController = undefined
  }
}

onMounted(() => void loadAlert())
watch(alertId, () => {
  if (import.meta.client) void loadAlert()
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
      :to="backToList"
    >
      <UIcon
        name="i-lucide-arrow-left"
        class="size-4"
        aria-hidden="true"
      />
      Back to alerts
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
      <span>Loading alert details…</span>
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
          Alert not found.
        </h1>
        <p>This alert does not exist in the development registry.</p>
        <NuxtLink
          class="primary-action"
          :to="backToList"
        >
          Back to alerts
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
          Alert could not be loaded.
        </h1>
        <p>{{ errorMessage }}</p>
        <button
          class="primary-action"
          type="button"
          @click="loadAlert"
        >
          Try again
        </button>
      </div>
    </div>

    <template v-else-if="state === 'ready' && alert">
      <p class="eyebrow">
        Alert occurrence
      </p>
      <h1
        id="page-title"
        ref="pageHeading"
        class="page-heading"
        tabindex="-1"
      >
        Triggering context
      </h1>
      <p class="page-intro alert-comparison">
        {{ comparisonText(alert) }}
      </p>

      <dl class="detail-card alert-detail-card">
        <div>
          <dt>Affected device</dt>
          <dd>
            <NuxtLink :to="devicePath(alert.deviceId)">{{ device?.displayName ?? alert.deviceId }}</NuxtLink>
          </dd>
          <p
            v-if="deviceLabelState === 'loading'"
            class="alert-detail-hint"
            role="status"
          >
            Loading device name…
          </p>
          <p
            v-else-if="deviceLabelState === 'unavailable'"
            class="alert-detail-hint"
          >
            Device name is unavailable; the stored device identifier remains available.
          </p>
        </div>
        <div>
          <dt>Device identifier</dt>
          <dd class="detail-id">
            {{ alert.deviceId }}
          </dd>
        </div>
        <div>
          <dt>Rule identifier</dt>
          <dd class="detail-id">
            {{ alert.ruleId }}
          </dd>
        </div>
        <div>
          <dt>Message identifier</dt>
          <dd class="detail-id">
            {{ alert.messageId }}
          </dd>
        </div>
        <div>
          <dt>Metric</dt>
          <dd>Temperature (°C) · {{ alert.metric }}</dd>
        </div>
        <div>
          <dt>Comparator</dt>
          <dd>{{ alert.comparator }}</dd>
        </div>
        <div>
          <dt>Measurement</dt>
          <dd>{{ formatNumber(alert.temperatureCelsius) }} °C</dd>
        </div>
        <div>
          <dt>Threshold</dt>
          <dd>{{ formatNumber(alert.thresholdCelsius) }} °C</dd>
        </div>
        <div>
          <dt>Observed</dt>
          <dd><time :datetime="alert.observedAt">{{ formatDate(alert.observedAt) }}</time></dd>
        </div>
        <div>
          <dt>Received</dt>
          <dd><time :datetime="alert.receivedAt">{{ formatDate(alert.receivedAt) }}</time></dd>
        </div>
        <div>
          <dt>Recorded</dt>
          <dd><time :datetime="alert.createdAt">{{ formatDate(alert.createdAt) }}</time></dd>
        </div>
      </dl>
    </template>
  </section>
</template>
