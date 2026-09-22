<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import {
  DeviceGraphQLError,
  getDeviceTelemetryOverview,
  getDeviceTelemetryPage,
  type DeviceCurrentState,
  type TelemetryPoint,
} from '../features/devices/device-graphql'

const props = defineProps<{ deviceId: string }>()

const PAGE_SIZE = 50
const STALE_AFTER_MS = 5 * 60 * 1000
const RECENCY_TICK_MS = 30 * 1000

type LoadState = 'loading' | 'ready' | 'error'
type SignalState = 'empty' | 'recent' | 'stale' | 'unknown' | 'inconsistent'

const state = ref<LoadState>('loading')
const currentState = ref<DeviceCurrentState | null>(null)
const points = ref<TelemetryPoint[]>([])
const endCursor = ref<string | null>(null)
const hasNextPage = ref(false)
const errorMessage = ref('')
const refreshError = ref('')
const loadMoreError = ref('')
const loadingMore = ref(false)
const refreshing = ref(false)
const now = ref(Date.now())

let requestController: AbortController | undefined
let requestSequence = 0
let recencyTimer: ReturnType<typeof setInterval> | undefined

const hasHistory = computed(() => points.value.length > 0)
const isBusy = computed(() => state.value === 'loading' || refreshing.value || loadingMore.value)

const signalState = computed<SignalState>(() => {
  if (state.value !== 'ready') return 'unknown'
  if (currentState.value === null && !hasHistory.value) return 'empty'
  if ((currentState.value === null) !== !hasHistory.value) return 'inconsistent'
  if (currentState.value === null) return 'empty'

  const lastSeen = Date.parse(currentState.value.lastSeenAt)
  if (!Number.isFinite(lastSeen)) return 'unknown'
  return Math.max(0, now.value - lastSeen) <= STALE_AFTER_MS ? 'recent' : 'stale'
})

const signalLabel = computed(() => {
  switch (signalState.value) {
    case 'empty': return 'No telemetry'
    case 'recent': return 'Recent signal'
    case 'stale': return 'Stale signal'
    case 'inconsistent': return 'Telemetry state is inconsistent'
    default: return 'Signal time unavailable'
  }
})

const signalDescription = computed(() => {
  switch (signalState.value) {
    case 'empty': return 'No telemetry has been committed for this device yet.'
    case 'recent': return `Last seen ${formatRelativeTime(currentState.value?.lastSeenAt ?? '')}.`
    case 'stale': return `Last seen ${formatRelativeTime(currentState.value?.lastSeenAt ?? '')}. Refresh to check for a newer signal.`
    case 'inconsistent': return 'The current-state and history responses do not agree. The available values remain visible for diagnosis.'
    default: return 'The last-seen time could not be interpreted safely.'
  }
})

const renderablePointCount = computed(() => points.value.filter(point => (
  Number.isFinite(Date.parse(point.observedAt)) && Number.isFinite(point.temperatureCelsius)
)).length)

const historySummary = computed(() => {
  const temperatures = points.value
    .map(point => point.temperatureCelsius)
    .filter(Number.isFinite)
  if (temperatures.length === 0) return 'Loaded telemetry values are not available for charting.'
  const minimum = Math.min(...temperatures)
  const maximum = Math.max(...temperatures)
  const range = minimum === maximum
    ? `${formatTemperature(minimum)}°C`
    : `${formatTemperature(minimum)}°C to ${formatTemperature(maximum)}°C`
  const latest = points.value[0]
  return `Loaded ${temperatures.length} observations ranging from ${range}. Newest observation ${formatDate(latest?.observedAt ?? '')}.`
})

function friendlyError(error: unknown): string {
  if (error instanceof DeviceGraphQLError && error.code === 'SERVICE_UNAVAILABLE') {
    return 'The telemetry service is unavailable. Try again.'
  }
  if (error instanceof DeviceGraphQLError && error.code === 'BAD_USER_INPUT') return error.message
  return 'Telemetry could not be loaded. Try again.'
}

function formatTemperature(value: number): string {
  return Number.isFinite(value)
    ? new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)
    : 'Unknown'
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? 'Unknown date'
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: 'medium',
        timeStyle: 'short',
      }).format(date)
}

function formatRelativeTime(value: string): string {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return 'an unknown time'
  const age = Math.max(0, now.value - timestamp)
  const minutes = Math.floor(age / 60_000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'} ago`
  const hours = Math.floor(minutes / 60)
  return `${hours} hour${hours === 1 ? '' : 's'} ago`
}

function resetTelemetry() {
  currentState.value = null
  points.value = []
  endCursor.value = null
  hasNextPage.value = false
}

async function loadOverview(refresh = false) {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence

  if (refresh) {
    refreshing.value = true
    loadingMore.value = false
    refreshError.value = ''
    loadMoreError.value = ''
  }
  else {
    state.value = 'loading'
    refreshing.value = false
    loadingMore.value = false
    errorMessage.value = ''
    refreshError.value = ''
    loadMoreError.value = ''
    resetTelemetry()
  }

  try {
    const result = await getDeviceTelemetryOverview(props.deviceId, PAGE_SIZE, null, controller.signal)
    if (sequence !== requestSequence) return
    currentState.value = result.deviceCurrentState
    points.value = result.deviceTelemetry.edges.map(edge => edge.node)
    endCursor.value = result.deviceTelemetry.pageInfo.endCursor
    hasNextPage.value = result.deviceTelemetry.pageInfo.hasNextPage
    state.value = 'ready'
    refreshError.value = ''
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== requestSequence) return
    if (refresh) refreshError.value = friendlyError(error)
    else {
      state.value = 'error'
      errorMessage.value = friendlyError(error)
    }
  }
  finally {
    if (sequence === requestSequence) {
      refreshing.value = false
      requestController = undefined
    }
  }
}

async function loadMore() {
  if (state.value !== 'ready' || loadingMore.value || !hasNextPage.value || endCursor.value === null) return

  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  loadingMore.value = true
  loadMoreError.value = ''

  try {
    const result = await getDeviceTelemetryPage(props.deviceId, PAGE_SIZE, endCursor.value, controller.signal)
    if (sequence !== requestSequence) return
    points.value = [...points.value, ...result.deviceTelemetry.edges.map(edge => edge.node)]
    endCursor.value = result.deviceTelemetry.pageInfo.endCursor
    hasNextPage.value = result.deviceTelemetry.pageInfo.hasNextPage
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== requestSequence) return
    loadMoreError.value = friendlyError(error)
  }
  finally {
    if (sequence === requestSequence) {
      loadingMore.value = false
      requestController = undefined
    }
  }
}

function refreshTelemetry() {
  if (state.value === 'loading' || refreshing.value) return
  void loadOverview(state.value === 'ready')
}

function retryLoadMore() {
  void loadMore()
}

watch(() => props.deviceId, () => {
  if (import.meta.client) void loadOverview()
})

onMounted(() => {
  now.value = Date.now()
  recencyTimer = setInterval(() => {
    now.value = Date.now()
  }, RECENCY_TICK_MS)
  void loadOverview()
})

onBeforeUnmount(() => {
  requestSequence += 1
  requestController?.abort()
  if (recencyTimer !== undefined) clearInterval(recencyTimer)
})
</script>

<template>
  <section
    class="telemetry-panel"
    aria-labelledby="telemetry-heading"
    :aria-busy="isBusy"
  >
    <div class="page-toolbar telemetry-toolbar">
      <div>
        <p class="eyebrow">
          Telemetry
        </p>
        <h2
          id="telemetry-heading"
          class="section-heading"
        >
          Current state and recent history
        </h2>
        <p class="page-intro">
          Review the latest committed temperature and the signals received for this device.
        </p>
      </div>
      <div class="page-actions">
        <button
          class="secondary-action"
          type="button"
          :disabled="state === 'loading' || refreshing"
          @click="refreshTelemetry"
        >
          <UIcon
            name="i-lucide-refresh-cw"
            class="size-4"
            aria-hidden="true"
          />
          Refresh telemetry
        </button>
      </div>
    </div>

    <p
      v-if="refreshing"
      class="telemetry-progress"
      role="status"
      aria-live="polite"
    >
      Refreshing telemetry…
    </p>

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
      <span>Loading telemetry…</span>
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
        <h3>Telemetry could not be loaded.</h3>
        <p>{{ errorMessage }}</p>
        <button
          class="primary-action"
          type="button"
          @click="refreshTelemetry"
        >
          Try again
        </button>
      </div>
    </div>

    <template v-else>
      <div
        v-if="refreshError"
        class="telemetry-alert"
        role="alert"
      >
        <UIcon
          name="i-lucide-circle-alert"
          class="size-5"
          aria-hidden="true"
        />
        <span>{{ refreshError }} The previous telemetry remains visible.</span>
      </div>

      <div
        v-if="signalState === 'empty'"
        class="state-panel state-panel--empty"
        role="status"
      >
        <UIcon
          name="i-lucide-activity"
          class="size-7"
          aria-hidden="true"
        />
        <div>
          <h3>No telemetry yet.</h3>
          <p>Publish a device observation, then refresh this page to inspect it.</p>
        </div>
      </div>

      <template v-else>
        <div
          v-if="signalState === 'inconsistent'"
          class="telemetry-alert"
          role="alert"
        >
          <UIcon
            name="i-lucide-triangle-alert"
            class="size-5"
            aria-hidden="true"
          />
          <span>{{ signalDescription }}</span>
        </div>

        <article
          v-if="currentState"
          class="telemetry-current-card"
          aria-labelledby="telemetry-current-heading"
        >
          <div class="telemetry-current-heading">
            <div>
              <p class="eyebrow">
                Current measurement
              </p>
              <h3 id="telemetry-current-heading">
                {{ formatTemperature(currentState.temperatureCelsius) }}°C
              </h3>
            </div>
            <span
              class="telemetry-status"
              :class="`telemetry-status--${signalState}`"
            >
              <UIcon
                :name="signalState === 'recent' ? 'i-lucide-radio' : 'i-lucide-clock-3'"
                class="size-4"
                aria-hidden="true"
              />
              {{ signalLabel }}
            </span>
          </div>
          <p class="telemetry-status-description">
            {{ signalDescription }}
          </p>
          <dl class="telemetry-metrics">
            <div>
              <dt>Observed</dt>
              <dd>{{ formatDate(currentState.observedAt) }}</dd>
            </div>
            <div>
              <dt>Received</dt>
              <dd>{{ formatDate(currentState.receivedAt) }}</dd>
            </div>
            <div>
              <dt>Last seen</dt>
              <dd>{{ formatDate(currentState.lastSeenAt) }}</dd>
            </div>
          </dl>
        </article>

        <section
          v-if="hasHistory"
          class="telemetry-history"
          aria-labelledby="telemetry-history-heading"
        >
          <div class="telemetry-section-heading">
            <div>
              <p class="eyebrow">
                Recent values
              </p>
              <h3 id="telemetry-history-heading">
                Temperature history
              </h3>
            </div>
            <p class="telemetry-count">
              {{ points.length }} loaded
            </p>
          </div>

          <div
            v-if="renderablePointCount >= 2"
            class="telemetry-chart-card"
          >
            <TelemetryTemperatureChart :points="points" />
            <p class="telemetry-chart-summary">
              {{ historySummary }}
            </p>
          </div>
          <p
            v-else
            class="telemetry-chart-summary telemetry-chart-summary--standalone"
          >
            {{ historySummary }}
          </p>

          <div class="telemetry-table-wrap">
            <table class="telemetry-table">
              <caption class="sr-only">
                Recent telemetry observations, newest first
              </caption>
              <thead>
                <tr>
                  <th scope="col">
                    Observed
                  </th>
                  <th scope="col">
                    Temperature
                  </th>
                  <th scope="col">
                    Received
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="point in points"
                  :key="point.messageId"
                >
                  <td>{{ formatDate(point.observedAt) }}</td>
                  <td>{{ formatTemperature(point.temperatureCelsius) }}°C</td>
                  <td>{{ formatDate(point.receivedAt) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <p
            v-if="loadMoreError"
            class="telemetry-alert telemetry-alert--inline"
            role="alert"
          >
            <UIcon
              name="i-lucide-circle-alert"
              class="size-5"
              aria-hidden="true"
            />
            <span>{{ loadMoreError }}</span>
            <button
              class="text-action"
              type="button"
              @click="retryLoadMore"
            >
              Retry history
            </button>
          </p>

          <button
            v-if="hasNextPage"
            class="secondary-action pagination-actions telemetry-load-more"
            type="button"
            :disabled="loadingMore"
            :aria-busy="loadingMore"
            @click="loadMore"
          >
            <UIcon
              v-if="loadingMore"
              name="i-lucide-loader-circle"
              class="size-4 animate-spin"
              aria-hidden="true"
            />
            {{ loadingMore ? 'Loading history…' : 'Load more history' }}
          </button>
        </section>
      </template>
    </template>
  </section>
</template>
