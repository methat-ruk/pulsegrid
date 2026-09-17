<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'

import {
  createDevice,
  DeviceGraphQLError,
  validateDeviceInput,
} from '../../features/devices/device-graphql'

useHead({ title: 'Add device · PulseGrid Console' })

const router = useRouter()
const deviceKey = ref('')
const displayName = ref('')
const fieldErrors = ref<Partial<Record<'deviceKey' | 'displayName', string>>>({})
const formError = ref('')
const submitting = ref(false)
let requestController: AbortController | undefined

function friendlyError(error: unknown): string {
  if (error instanceof DeviceGraphQLError && error.code === 'CONFLICT') {
    return 'A device with this key already exists.'
  }
  if (error instanceof DeviceGraphQLError && error.code === 'SERVICE_UNAVAILABLE') {
    return 'The device service is unavailable. Try again.'
  }
  if (error instanceof DeviceGraphQLError && error.code === 'BAD_USER_INPUT') return error.message
  return 'The device could not be created. Try again.'
}

async function submit() {
  if (submitting.value) return
  const input = { deviceKey: deviceKey.value, displayName: displayName.value }
  fieldErrors.value = validateDeviceInput(input)
  formError.value = ''
  if (Object.keys(fieldErrors.value).length > 0) return

  submitting.value = true
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  try {
    const result = await createDevice(input, controller.signal)
    await router.push(`/devices/${encodeURIComponent(result.createDevice.id)}`)
  }
  catch (error) {
    if (!controller.signal.aborted) formError.value = friendlyError(error)
  }
  finally {
    if (requestController === controller) {
      requestController = undefined
      submitting.value = false
    }
  }
}

onBeforeUnmount(() => requestController?.abort())
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
    <p class="eyebrow">
      Registry
    </p>
    <h1
      id="page-title"
      class="page-heading"
    >
      Add device
    </h1>
    <p class="page-intro">
      Provision a device in the development registry.
    </p>

    <form
      class="form-card"
      novalidate
      @submit.prevent="submit"
    >
      <div
        v-if="formError"
        class="form-alert"
        role="alert"
      >
        <UIcon
          name="i-lucide-circle-alert"
          class="size-5"
          aria-hidden="true"
        />
        <span>{{ formError }}</span>
      </div>

      <div class="field-group">
        <label for="device-key">Device key</label>
        <p class="field-help">
          A stable identifier with no leading or trailing whitespace.
        </p>
        <input
          id="device-key"
          v-model="deviceKey"
          name="deviceKey"
          type="text"
          autocomplete="off"
          :aria-invalid="fieldErrors.deviceKey ? 'true' : undefined"
          :aria-describedby="fieldErrors.deviceKey ? 'device-key-error' : 'device-key-help'"
        >
        <span
          id="device-key-help"
          class="sr-only"
        >128 characters maximum.</span>
        <span
          v-if="fieldErrors.deviceKey"
          id="device-key-error"
          class="field-error"
        >{{ fieldErrors.deviceKey }}</span>
      </div>

      <div class="field-group">
        <label for="display-name">Display name</label>
        <p
          id="display-name-help"
          class="field-help"
        >
          A human-readable name for operators. 200 characters maximum.
        </p>
        <input
          id="display-name"
          v-model="displayName"
          name="displayName"
          type="text"
          :aria-invalid="fieldErrors.displayName ? 'true' : undefined"
          :aria-describedby="fieldErrors.displayName ? 'display-name-error' : 'display-name-help'"
        >
        <span
          v-if="fieldErrors.displayName"
          id="display-name-error"
          class="field-error"
        >{{ fieldErrors.displayName }}</span>
      </div>

      <div class="form-actions">
        <NuxtLink
          class="secondary-action"
          to="/devices"
        >Cancel</NuxtLink>
        <button
          class="primary-action"
          type="submit"
          :disabled="submitting"
          :aria-busy="submitting"
        >
          <UIcon
            v-if="submitting"
            name="i-lucide-loader-circle"
            class="size-4 animate-spin"
            aria-hidden="true"
          />
          {{ submitting ? 'Creating…' : 'Create device' }}
        </button>
      </div>
    </form>
  </section>
</template>
