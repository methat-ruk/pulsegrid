<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { listAlerts, type AlertConnection, type AlertOccurrence } from '../../features/alerts/alert-graphql'
import { GraphQLClientError } from '../../features/graphql/client'
import { getDevice, type Device } from '../../features/devices/device-graphql'

const PAGE_SIZE = 50
const route = useRoute()
const alerts = ref<AlertOccurrence[]>([])
const device = ref<Device | null>(null)
const state = ref<'loading' | 'device-unavailable' | 'error' | 'ready'>('loading')
const initialError = ref('')
const refreshError = ref('')
const pageError = ref('')
const endCursor = ref<string | null>(null)
const hasNextPage = ref(false)
const refreshing = ref(false)
const loadingMore = ref(false)
const pageHeadingElement = ref<HTMLElement | null>(null)

let requestController: AbortController | undefined
let requestSequence = 0

const rawDeviceFilter = computed(() => route.query.deviceId)
const deviceFilter = computed(() => {
  if (rawDeviceFilter.value === undefined) return null
  return typeof rawDeviceFilter.value === 'string' ? rawDeviceFilter.value : ''
})
const pageHeading = computed(() => device.value ? `Alerts for ${device.value.displayName}` : 'Alert occurrences')
const isBusy = computed(() => state.value === 'loading' || refreshing.value || loadingMore.value)

useHead(() => ({ title: `${pageHeading.value} · PulseGrid Console` }))

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? 'Unknown date'
    : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatNumber(value: number): string {
  return Number.isFinite(value) ? String(value) : 'Unknown'
}

function comparisonText(alert: Pick<AlertOccurrence, 'metric' | 'temperatureCelsius' | 'comparator' | 'thresholdCelsius'>): string {
  const metric = alert.metric === 'TEMPERATURE_CELSIUS' ? 'Temperature' : 'Measurement'
  const comparison = {
    GT: 'is greater than',
    GTE: 'is greater than or equal to',
    LT: 'is less than',
    LTE: 'is less than or equal to',
  }[alert.comparator]
  return `${metric}: ${formatNumber(alert.temperatureCelsius)} °C ${comparison} ${formatNumber(alert.thresholdCelsius)} °C`
}

function alertDetailPath(id: string): string {
  const path = `/alerts/${encodeURIComponent(id)}`
  return deviceFilter.value === null
    ? path
    : `${path}?deviceId=${encodeURIComponent(deviceFilter.value)}`
}

function devicePath(id: string): string {
  return `/devices/${encodeURIComponent(id)}`
}

async function focusPageHeading() {
  await nextTick()
  pageHeadingElement.value?.focus()
}

function resetPage() {
  alerts.value = []
  endCursor.value = null
  hasNextPage.value = false
  initialError.value = ''
  refreshError.value = ''
  pageError.value = ''
  refreshing.value = false
  loadingMore.value = false
}

async function loadScope() {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  const filter = deviceFilter.value
  device.value = null
  resetPage()
  state.value = 'loading'

  if (filter !== null) {
    try {
      const result = await getDevice(filter, controller.signal)
      if (sequence !== requestSequence) return
      if (result.device === null) {
        state.value = 'device-unavailable'
        await focusPageHeading()
        return
      }
      device.value = result.device
    }
    catch (error) {
      if (controller.signal.aborted || sequence !== requestSequence) return
      if (error instanceof GraphQLClientError && error.code === 'BAD_USER_INPUT') {
        state.value = 'device-unavailable'
        await focusPageHeading()
        return
      }
      state.value = 'error'
      initialError.value = 'The alert view could not be loaded. Try again.'
      await focusPageHeading()
      return
    }
  }

  await loadFirstPage(sequence, controller, filter)
  if (sequence === requestSequence) await focusPageHeading()
}

async function loadFirstPage(sequence: number, controller: AbortController, filter: string | null) {
  try {
    const result = await listAlerts(PAGE_SIZE, null, filter, controller.signal)
    if (sequence !== requestSequence) return
    applyPage(result.alerts, false)
    state.value = 'ready'
  }
  catch {
    if (controller.signal.aborted || sequence !== requestSequence) return
    state.value = 'error'
    initialError.value = 'The alert service is unavailable. Try again.'
  }
  finally {
    if (sequence === requestSequence) requestController = undefined
  }
}

function applyPage(connection: AlertConnection, append: boolean) {
  const page = connection.edges.map(edge => edge.node)
  alerts.value = append ? [...alerts.value, ...page] : page
  endCursor.value = connection.pageInfo.endCursor
  hasNextPage.value = connection.pageInfo.hasNextPage
}

async function retryInitialLoad() {
  await loadScope()
}

async function refreshAlerts() {
  if (state.value !== 'ready' || refreshing.value) return
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  refreshing.value = true
  loadingMore.value = false
  refreshError.value = ''
  pageError.value = ''

  try {
    const result = await listAlerts(PAGE_SIZE, null, deviceFilter.value, controller.signal)
    if (sequence !== requestSequence) return
    applyPage(result.alerts, false)
  }
  catch {
    if (controller.signal.aborted || sequence !== requestSequence) return
    refreshError.value = alerts.value.length > 0
      ? 'Alerts could not be refreshed. Previously loaded occurrences remain visible.'
      : 'Alerts could not be refreshed. The last loaded list is still empty.'
  }
  finally {
    if (sequence === requestSequence) {
      refreshing.value = false
      requestController = undefined
    }
  }
}

async function loadMore() {
  if (state.value !== 'ready' || refreshing.value || loadingMore.value || !hasNextPage.value || endCursor.value === null) return
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  const cursor = endCursor.value
  loadingMore.value = true
  pageError.value = ''

  try {
    const result = await listAlerts(PAGE_SIZE, cursor, deviceFilter.value, controller.signal)
    if (sequence !== requestSequence) return
    applyPage(result.alerts, true)
  }
  catch {
    if (controller.signal.aborted || sequence !== requestSequence) return
    pageError.value = 'The next page could not be loaded. Previously loaded occurrences remain visible.'
  }
  finally {
    if (sequence === requestSequence) {
      loadingMore.value = false
      requestController = undefined
    }
  }
}

function retryLoadMore() {
  void loadMore()
}

onMounted(() => void loadScope())
watch(deviceFilter, () => {
  if (import.meta.client) void loadScope()
})
onBeforeUnmount(() => {
  requestSequence += 1
  requestController?.abort()
})
</script>

<template>
  <section
    aria-labelledby="page-title"
    :aria-busy="isBusy"
  >
    <div class="page-toolbar">
      <div>
        <p class="eyebrow">
          Operations
        </p>
        <h1
          id="page-title"
          ref="pageHeadingElement"
          class="page-heading"
          tabindex="-1"
        >
          {{ pageHeading }}
        </h1>
        <p class="page-intro">
          Review recent threshold occurrences and their stored measurement context.
        </p>
      </div>
      <div
        v-if="state === 'ready'"
        class="page-actions"
      >
        <button
          class="secondary-action"
          type="button"
          :disabled="refreshing"
          :aria-busy="refreshing"
          @click="refreshAlerts"
        >
          <UIcon
            v-if="refreshing"
            name="i-lucide-loader-circle"
            class="size-4 animate-spin"
            aria-hidden="true"
          />
          {{ refreshing ? 'Refreshing…' : 'Refresh alerts' }}
        </button>
      </div>
    </div>

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
      <span>{{ deviceFilter === null ? 'Loading alerts…' : 'Loading device alerts…' }}</span>
    </div>

    <div
      v-else-if="state === 'device-unavailable'"
      class="state-panel state-panel--empty"
      role="status"
    >
      <UIcon
        name="i-lucide-search-x"
        class="size-7"
        aria-hidden="true"
      />
      <div>
        <h2>Device alerts are unavailable.</h2>
        <p>This device could not be found in the development registry.</p>
        <NuxtLink
          class="primary-action"
          to="/alerts"
        >
          View all alerts
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
        <h2>Alerts could not be loaded.</h2>
        <p>{{ initialError }}</p>
        <button
          class="primary-action"
          type="button"
          @click="retryInitialLoad"
        >
          Try again
        </button>
      </div>
    </div>

    <template v-else-if="state === 'ready'">
      <div
        v-if="refreshing"
        class="telemetry-progress"
        role="status"
        aria-live="polite"
      >
        <UIcon
          name="i-lucide-loader-circle"
          class="size-4 animate-spin"
          aria-hidden="true"
        />
        <span>Refreshing alerts. Previously loaded occurrences remain visible.</span>
      </div>

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
        <div>
          <p>{{ refreshError }}</p>
          <button
            class="text-action"
            type="button"
            @click="refreshAlerts"
          >
            Retry refresh
          </button>
        </div>
      </div>

      <div
        v-if="alerts.length === 0"
        class="state-panel state-panel--empty"
        role="status"
      >
        <UIcon
          name="i-lucide-bell"
          class="size-7"
          aria-hidden="true"
        />
        <div>
          <h2>No alert occurrences yet.</h2>
          <p>Matching threshold conditions will appear here after telemetry is accepted.</p>
        </div>
      </div>

      <template v-else>
        <div class="alert-table-wrap">
          <table class="alert-table">
            <caption class="sr-only">
              Recent alert occurrences, newest recorded first
            </caption>
            <thead>
              <tr>
                <th scope="col">
                  Recorded
                </th>
                <th scope="col">
                  Triggering comparison
                </th>
                <th scope="col">
                  Device
                </th>
                <th scope="col">
                  <span class="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="alert in alerts"
                :key="alert.id"
              >
                <td><time :datetime="alert.createdAt">{{ formatDate(alert.createdAt) }}</time></td>
                <td>{{ comparisonText(alert) }}</td>
                <td>
                  <NuxtLink :to="devicePath(alert.deviceId)">{{ alert.deviceId }}</NuxtLink>
                </td>
                <td>
                  <NuxtLink
                    class="text-action"
                    :to="alertDetailPath(alert.id)"
                  >View details</NuxtLink>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <ul
          class="alert-card-list"
          aria-label="Recent alert occurrences, newest recorded first"
        >
          <li
            v-for="alert in alerts"
            :key="alert.id"
            class="alert-card"
          >
            <p class="eyebrow">
              Recorded
            </p>
            <time :datetime="alert.createdAt">{{ formatDate(alert.createdAt) }}</time>
            <p class="alert-card-comparison">
              {{ comparisonText(alert) }}
            </p>
            <dl>
              <div>
                <dt>Device</dt>
                <dd><NuxtLink :to="devicePath(alert.deviceId)">{{ alert.deviceId }}</NuxtLink></dd>
              </div>
            </dl>
            <NuxtLink
              class="text-action"
              :to="alertDetailPath(alert.id)"
            >View alert details</NuxtLink>
          </li>
        </ul>

        <div
          v-if="pageError"
          class="telemetry-alert"
          role="alert"
        >
          <UIcon
            name="i-lucide-circle-alert"
            class="size-5"
            aria-hidden="true"
          />
          <div>
            <p>{{ pageError }}</p>
            <button
              class="text-action"
              type="button"
              @click="retryLoadMore"
            >
              Retry loading more
            </button>
          </div>
        </div>

        <div
          v-if="hasNextPage"
          class="pagination-actions"
        >
          <button
            class="secondary-action"
            type="button"
            :disabled="loadingMore || refreshing || endCursor === null"
            :aria-busy="loadingMore"
            @click="loadMore"
          >
            <UIcon
              v-if="loadingMore"
              name="i-lucide-loader-circle"
              class="size-4 animate-spin"
              aria-hidden="true"
            />
            {{ loadingMore ? 'Loading…' : 'Load more alerts' }}
          </button>
        </div>
      </template>
    </template>
  </section>
</template>
