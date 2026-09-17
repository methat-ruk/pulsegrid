<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

import {
  DeviceGraphQLError,
  type Device,
  listDevices,
} from '../../features/devices/device-graphql'

useHead({ title: 'Devices · PulseGrid Console' })

const pageSize = 20
const devices = ref<Device[]>([])
const endCursor = ref<string | null>(null)
const hasNextPage = ref(false)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const loadingMore = ref(false)
const errorMessage = ref('')
let requestController: AbortController | undefined
let requestSequence = 0

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
  return 'The device list could not be loaded. Try again.'
}

async function loadDevices(after: string | null = null, append = false) {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  if (append) loadingMore.value = true
  else {
    state.value = 'loading'
    errorMessage.value = ''
    if (after === null) devices.value = []
  }

  try {
    const result = await listDevices(pageSize, after, controller.signal)
    if (sequence !== requestSequence) return
    devices.value = append ? [...devices.value, ...result.devices.edges.map(edge => edge.node)] : result.devices.edges.map(edge => edge.node)
    endCursor.value = result.devices.pageInfo.endCursor
    hasNextPage.value = result.devices.pageInfo.hasNextPage
    state.value = 'ready'
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== requestSequence) return
    state.value = 'error'
    errorMessage.value = friendlyError(error)
  }
  finally {
    if (sequence === requestSequence) loadingMore.value = false
  }
}

function loadMore() {
  if (!hasNextPage.value || loadingMore.value || state.value !== 'ready' || endCursor.value === null) return
  void loadDevices(endCursor.value, true)
}

onMounted(() => void loadDevices())
onBeforeUnmount(() => {
  requestSequence += 1
  requestController?.abort()
})
</script>

<template>
  <section aria-labelledby="page-title">
    <div class="page-toolbar">
      <div>
        <p class="eyebrow">
          Registry
        </p>
        <h1
          id="page-title"
          class="page-heading"
        >
          Devices
        </h1>
        <p class="page-intro">
          Manage devices for the development organization.
        </p>
      </div>
      <div class="page-actions">
        <button
          class="secondary-action"
          type="button"
          :disabled="state === 'loading'"
          @click="loadDevices()"
        >
          <UIcon
            name="i-lucide-refresh-cw"
            class="size-4"
            aria-hidden="true"
          />
          Refresh
        </button>
        <NuxtLink
          class="primary-action"
          to="/devices/new"
        >
          <UIcon
            name="i-lucide-plus"
            class="size-4"
            aria-hidden="true"
          />
          Add device
        </NuxtLink>
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
      <span>Loading devices…</span>
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
        <h2>Devices could not be loaded.</h2>
        <p>{{ errorMessage }}</p>
        <button
          class="primary-action"
          type="button"
          @click="loadDevices()"
        >
          Try again
        </button>
      </div>
    </div>

    <div
      v-else-if="devices.length === 0"
      class="state-panel state-panel--empty"
    >
      <UIcon
        name="i-lucide-cpu"
        class="size-7"
        aria-hidden="true"
      />
      <div>
        <h2>No devices yet.</h2>
        <p>Provision the first device to start the registry.</p>
        <NuxtLink
          class="primary-action"
          to="/devices/new"
        >
          Add device
        </NuxtLink>
      </div>
    </div>

    <template v-else>
      <div class="device-table-wrap">
        <table class="device-table">
          <caption class="sr-only">
            Registered devices
          </caption>
          <thead>
            <tr>
              <th scope="col">
                Device key
              </th>
              <th scope="col">
                Display name
              </th>
              <th scope="col">
                Created
              </th>
              <th scope="col">
                <span class="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="device in devices"
              :key="device.id"
            >
              <th scope="row">
                <NuxtLink :to="`/devices/${encodeURIComponent(device.id)}`">{{ device.deviceKey }}</NuxtLink>
              </th>
              <td>{{ device.displayName }}</td>
              <td>{{ formatDate(device.createdAt) }}</td>
              <td>
                <NuxtLink
                  class="text-action"
                  :to="`/devices/${encodeURIComponent(device.id)}`"
                >
                  View
                </NuxtLink>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="device-card-list">
        <article
          v-for="device in devices"
          :key="device.id"
          class="device-card"
        >
          <div class="device-card-heading">
            <p class="eyebrow">
              Device
            </p>
            <NuxtLink
              class="text-action"
              :to="`/devices/${encodeURIComponent(device.id)}`"
            >
              View
            </NuxtLink>
          </div>
          <h2 class="device-card-key">
            {{ device.deviceKey }}
          </h2>
          <p class="device-card-name">
            {{ device.displayName }}
          </p>
          <p class="device-card-date">
            Created {{ formatDate(device.createdAt) }}
          </p>
        </article>
      </div>

      <div
        v-if="hasNextPage"
        class="pagination-actions"
      >
        <button
          class="secondary-action"
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
          {{ loadingMore ? 'Loading…' : 'Load more' }}
        </button>
      </div>
    </template>
  </section>
</template>
