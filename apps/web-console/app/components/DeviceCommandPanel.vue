<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { GraphQLClientError } from '../features/graphql/client'
import {
  createCommand,
  DEFAULT_COMMAND_PAGE_SIZE,
  getCommand,
  isTerminalCommand,
  listDeviceCommands,
  type CommandSnapshot,
  type CommandStatus,
} from '../features/commands/command-graphql'
import {
  createCommandIntent,
  forgetUnusableCommandIntent,
  readCommandIntent,
  removeCommandIntent,
  writeCommandIntent,
  type CommandIntent,
} from '../features/commands/command-recovery'

const props = defineProps<{ deviceId: string, deviceKey: string }>()

const POLL_INTERVAL_MS = 2_000
const MAX_AUTOMATIC_READS = 75
const MAX_TRACKING_MS = 150_000

type HistoryState = 'loading' | 'ready' | 'error'
type RecoveryIssue = 'invalid' | 'unavailable' | null
type TrackingState = 'idle' | 'tracking' | 'paused' | 'error'

const historyState = ref<HistoryState>('loading')
const historyError = ref('')
const historyRefreshing = ref(false)
const commandRows = ref<CommandSnapshot[]>([])
const hasOlderCommands = ref(false)
const selectedCommand = ref<CommandSnapshot | null>(null)
const selectedCommandId = ref<string | null>(null)
const selectedCommandMissing = ref(false)
const selectedCommandError = ref('')
const selectedReadAt = ref<number | null>(null)
const selectedReadPending = ref(false)
const confirming = ref(false)
const submitting = ref(false)
const submissionError = ref('')
const submissionUnknown = ref(false)
const recoveryIssue = ref<RecoveryIssue>(null)
const intent = ref<CommandIntent | null>(null)
const intentResolved = ref(false)
const intentCleanupError = ref(false)
const trackingState = ref<TrackingState>('idle')
const trackingMessage = ref('')

const terminal = computed(() => selectedCommand.value !== null && isTerminalCommand(selectedCommand.value.status))
const historyCountDescription = computed(() => hasOlderCommands.value
  ? `Older commands exist but are not shown. Showing at most the latest ${DEFAULT_COMMAND_PAGE_SIZE}.`
  : `Showing at most the latest ${DEFAULT_COMMAND_PAGE_SIZE} commands.`)
const canStartNewIntent = computed(() => recoveryIssue.value === null
  && intent.value === null
  && !submitting.value
  && !selectedReadPending.value)
const canRecoverIntent = computed(() => intent.value !== null
  && intent.value.commandId === null
  && !submitting.value
  && recoveryIssue.value === null)

const statusLabels: Record<CommandStatus, string> = {
  PENDING: 'Pending',
  DISPATCHED: 'Dispatched',
  ACKNOWLEDGED: 'Acknowledged',
  COMPLETED: 'Completed',
  FAILED: 'Failed',
  TIMED_OUT: 'Timed out',
}

let historyController: AbortController | undefined
let detailController: AbortController | undefined
let submissionController: AbortController | undefined
let historySequence = 0
let detailSequence = 0
let trackingTimer: ReturnType<typeof setTimeout> | undefined
let pollTimer: ReturnType<typeof setTimeout> | undefined
let trackingStartedAt = 0
let automaticReadCount = 0
let currentDetailIsAutomatic = false
let visibilityPaused = false

function statusDescription(status: CommandStatus): string {
  switch (status) {
    case 'PENDING': return 'The intent is stored. Device delivery is not confirmed yet.'
    case 'DISPATCHED': return 'The broker accepted the publish. Device receipt is not confirmed yet.'
    case 'ACKNOWLEDGED': return 'The device reported receipt. Its final result is still pending.'
    case 'COMPLETED': return 'The device reported that the diagnostic PING completed.'
    case 'FAILED': return selectedCommand.value?.failureCode === 'DEVICE_REPORTED_FAILURE'
      ? 'The device reported a failure while handling the PING.'
      : 'The command could not be delivered.'
    case 'TIMED_OUT': return 'The server recorded that no terminal device result arrived before the deadline.'
  }
}

function statusClass(status: CommandStatus): string {
  switch (status) {
    case 'COMPLETED': return 'command-status--success'
    case 'FAILED': return 'command-status--failure'
    case 'TIMED_OUT': return 'command-status--timeout'
    case 'PENDING': return 'command-status--pending'
    case 'DISPATCHED': return 'command-status--dispatched'
    case 'ACKNOWLEDGED': return 'command-status--acknowledged'
  }
}

function formatDate(value: string | null): string {
  if (value === null) return 'Not recorded'
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return 'Time unavailable'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function formatCommandId(value: string): string {
  return value
}

function safeSubmissionError(error: unknown): string {
  if (error instanceof GraphQLClientError) {
    if (error.code === 'BAD_USER_INPUT') return 'The server rejected this command input before storing it. Check that the device is still registered.'
    if (error.code === 'CONFLICT') return 'The server reported a conflict for this saved command. Its recovery key is preserved; review command history before continuing.'
  }
  return 'The submission result could not be confirmed. Keep this page open or return to this device and recover the same submission.'
}

function safeReadError(error: unknown): string {
  if (error instanceof GraphQLClientError && error.code === 'SERVICE_UNAVAILABLE') {
    return 'The command service is unavailable. Refresh to check the stored result.'
  }
  return 'The command status could not be refreshed. The last confirmed status remains visible.'
}

function clearTimer(timer: ReturnType<typeof setTimeout> | undefined): undefined {
  if (timer !== undefined) clearTimeout(timer)
  return undefined
}

function clearTrackingTimers() {
  pollTimer = clearTimer(pollTimer)
  trackingTimer = clearTimer(trackingTimer)
}

function stopTracking(state: TrackingState = 'idle', message = '') {
  clearTrackingTimers()
  trackingState.value = state
  trackingMessage.value = message
  visibilityPaused = false
}

function abortDetailRead() {
  detailSequence += 1
  detailController?.abort()
  detailController = undefined
  currentDetailIsAutomatic = false
  selectedReadPending.value = false
}

function trackingBudgetExpired(): boolean {
  return automaticReadCount >= MAX_AUTOMATIC_READS || performance.now() - trackingStartedAt >= MAX_TRACKING_MS
}

function pauseForBudget() {
  stopTracking('paused', 'Tracking paused — result not yet confirmed. Refresh the command manually or resume bounded tracking.')
  if (currentDetailIsAutomatic) abortDetailRead()
}

function armTrackingDeadline() {
  trackingTimer = clearTimer(trackingTimer)
  const remaining = Math.max(0, MAX_TRACKING_MS - (performance.now() - trackingStartedAt))
  trackingTimer = setTimeout(() => {
    if (trackingState.value === 'tracking') pauseForBudget()
  }, remaining)
}

function scheduleNextPoll() {
  if (trackingState.value !== 'tracking' || visibilityPaused) return
  if (trackingBudgetExpired()) {
    pauseForBudget()
    return
  }
  const remaining = MAX_TRACKING_MS - (performance.now() - trackingStartedAt)
  pollTimer = clearTimer(pollTimer)
  pollTimer = setTimeout(() => {
    if (trackingState.value !== 'tracking') return
    if (trackingBudgetExpired()) {
      pauseForBudget()
      return
    }
    void readSelectedCommand(true)
  }, Math.min(POLL_INTERVAL_MS, Math.max(0, remaining)))
}

function startTracking() {
  if (selectedCommand.value === null || isTerminalCommand(selectedCommand.value.status)) {
    stopTracking()
    return
  }
  stopTracking('tracking', 'Automatically checking the server status every 2 seconds. Tracking stops after the bounded check window.')
  trackingStartedAt = performance.now()
  automaticReadCount = 0
  armTrackingDeadline()
  void readSelectedCommand(true)
}

function startRecoveredTrackingAfterRead() {
  stopTracking('tracking', 'Automatically checking the recovered command every 2 seconds. Tracking stops after the bounded check window.')
  trackingStartedAt = performance.now()
  automaticReadCount = 1
  armTrackingDeadline()
  scheduleNextPoll()
}

function applySnapshot(snapshot: CommandSnapshot, requestSequence: number) {
  if (requestSequence !== detailSequence || snapshot.id !== selectedCommandId.value) return false
  const current = selectedCommand.value
  if (current !== null) {
    const currentUpdatedAt = Date.parse(current.updatedAt)
    const nextUpdatedAt = Date.parse(snapshot.updatedAt)
    if (nextUpdatedAt < currentUpdatedAt) return false
    if (nextUpdatedAt === currentUpdatedAt && snapshot.status !== current.status) {
      selectedCommandError.value = 'The latest command state is inconsistent. Refresh to confirm it.'
      stopTracking('error')
      return false
    }
  }

  selectedCommand.value = snapshot
  selectedCommandMissing.value = false
  selectedCommandError.value = ''
  selectedReadAt.value = Date.now()

  if (intent.value?.commandId === snapshot.id && isTerminalCommand(snapshot.status)) {
    resolveIntent(snapshot)
  }
  return true
}

function resolveIntent(snapshot: CommandSnapshot) {
  if (intent.value === null || intent.value.commandId !== snapshot.id || !isTerminalCommand(snapshot.status)) return
  intentResolved.value = true
  if (removeCommandIntent(intent.value)) {
    intent.value = null
    intentResolved.value = false
    intentCleanupError.value = false
    recoveryIssue.value = null
    submissionUnknown.value = false
    submissionError.value = ''
  }
  else {
    intentCleanupError.value = true
    submissionError.value = 'The server stored this terminal result, but browser recovery data could not be cleared. Retry cleanup before starting another PING.'
  }
}

async function readSelectedCommand(automatic = false, startRecoveredTracking = false): Promise<void> {
  const commandId = selectedCommandId.value
  if (commandId === null || selectedReadPending.value) return
  if (automatic && (trackingState.value !== 'tracking' || trackingBudgetExpired())) {
    if (trackingState.value === 'tracking') pauseForBudget()
    return
  }
  const sequence = ++detailSequence
  const controller = new AbortController()
  detailController = controller
  currentDetailIsAutomatic = automatic
  selectedReadPending.value = true
  if (automatic) automaticReadCount += 1

  try {
    const snapshot = await getCommand(commandId, props.deviceId, controller.signal)
    if (sequence !== detailSequence || selectedCommandId.value !== commandId) return
    if (snapshot === null) {
      selectedCommandMissing.value = true
      selectedCommandError.value = 'This command is unavailable. Its result remains unknown; a new PING is disabled while its recovery record exists.'
      stopTracking('error')
      return
    }
    const applied = applySnapshot(snapshot, sequence)
    if (!applied) {
      if (automatic && trackingState.value === 'tracking') scheduleNextPoll()
      return
    }
    if (isTerminalCommand(snapshot.status)) {
      stopTracking()
      void loadCommandHistory()
      return
    }
    if (automatic) scheduleNextPoll()
    else if (startRecoveredTracking && intent.value?.commandId === snapshot.id) startRecoveredTrackingAfterRead()
  }
  catch (error) {
    if (sequence !== detailSequence || controller.signal.aborted) return
    selectedCommandError.value = safeReadError(error)
    if (automatic) stopTracking('error', 'Automatic tracking stopped after a read error. Refresh the status, then resume if it is still pending.')
  }
  finally {
    if (sequence === detailSequence) {
      selectedReadPending.value = false
      detailController = undefined
      currentDetailIsAutomatic = false
    }
  }
}

function selectCommand(snapshot: CommandSnapshot) {
  stopTracking()
  abortDetailRead()
  selectedCommandId.value = snapshot.id
  selectedCommand.value = snapshot
  selectedCommandMissing.value = false
  selectedCommandError.value = ''
  selectedReadAt.value = null
  if (!isTerminalCommand(snapshot.status)) startTracking()
  else void readSelectedCommand()
}

function selectCommandId(commandId: string) {
  stopTracking()
  abortDetailRead()
  selectedCommandId.value = commandId
  selectedCommand.value = null
  selectedCommandMissing.value = false
  selectedCommandError.value = ''
  selectedReadAt.value = null
  void readSelectedCommand(false, true)
}

async function loadCommandHistory(initial = false) {
  if (historyRefreshing.value) return
  historyController?.abort()
  const controller = new AbortController()
  historyController = controller
  const sequence = ++historySequence
  historyRefreshing.value = true
  if (initial) historyState.value = 'loading'
  historyError.value = ''

  try {
    const result = await listDeviceCommands(props.deviceId, controller.signal)
    if (sequence !== historySequence) return
    hasOlderCommands.value = result.deviceCommands.pageInfo.hasNextPage
    const rows = result.deviceCommands.edges.map(edge => edge.node)
    const selectedIndex = rows.findIndex(command => command.id === selectedCommandId.value)
    const currentSelection = selectedCommand.value
    if (selectedIndex !== -1 && currentSelection?.id === selectedCommandId.value) {
      const historySelection = rows[selectedIndex]
      if (historySelection !== undefined) {
        const historyIsNewer = Date.parse(historySelection.updatedAt) > Date.parse(currentSelection.updatedAt)
        const historyConflictsAtSameTime = Date.parse(historySelection.updatedAt) === Date.parse(currentSelection.updatedAt)
          && historySelection.status !== currentSelection.status
        const latestSnapshot = historyIsNewer ? historySelection : currentSelection
        rows[selectedIndex] = latestSnapshot
        if (historyConflictsAtSameTime) {
          selectedCommandError.value = 'Command history conflicts with this status snapshot. Refresh the selected command to confirm the latest result.'
        }
        else if (latestSnapshot !== currentSelection) {
          selectedCommand.value = latestSnapshot
          selectedReadAt.value = Date.now()
          selectedCommandError.value = ''
          if (isTerminalCommand(latestSnapshot.status)) {
            stopTracking()
            resolveIntent(latestSnapshot)
          }
        }
      }
    }
    commandRows.value = rows
    historyState.value = 'ready'
    if (selectedCommandId.value === null && rows[0] !== undefined) selectCommand(rows[0])
  }
  catch (error) {
    if (controller.signal.aborted || sequence !== historySequence) return
    historyError.value = safeReadError(error)
    if (initial) historyState.value = 'error'
  }
  finally {
    if (sequence === historySequence) {
      historyRefreshing.value = false
      historyController = undefined
    }
  }
}

function hydrateIntentForDevice() {
  intent.value = null
  intentResolved.value = false
  intentCleanupError.value = false
  recoveryIssue.value = null
  submissionUnknown.value = false
  const stored = readCommandIntent(props.deviceId)
  if (stored.kind === 'invalid' || stored.kind === 'unavailable') {
    recoveryIssue.value = stored.kind
    return
  }
  if (stored.kind === 'empty') return
  intent.value = stored.intent
  if (stored.intent.commandId !== null) selectCommandId(stored.intent.commandId)
}

function initializePanel() {
  stopTracking()
  abortDetailRead()
  historyController?.abort()
  historySequence += 1
  historyRefreshing.value = false
  commandRows.value = []
  hasOlderCommands.value = false
  selectedCommandId.value = null
  selectedCommand.value = null
  selectedCommandMissing.value = false
  selectedCommandError.value = ''
  selectedReadAt.value = null
  historyState.value = 'loading'
  historyError.value = ''
  confirming.value = false
  submitting.value = false
  submissionError.value = ''
  submissionUnknown.value = false
  hydrateIntentForDevice()
  void loadCommandHistory(true)
}

async function confirmNewCommand() {
  if (!canStartNewIntent.value || submitting.value) return
  if (typeof window.crypto?.randomUUID !== 'function') {
    submissionError.value = 'This browser cannot create a safe command recovery key. No command was sent.'
    return
  }
  const newIntent = createCommandIntent(props.deviceId, window.crypto.randomUUID().toLowerCase())
  if (newIntent === null || !writeCommandIntent(newIntent)) {
    recoveryIssue.value = 'unavailable'
    submissionError.value = 'Command recovery storage is unavailable. No command was sent.'
    return
  }
  confirming.value = false
  submissionError.value = ''
  submissionUnknown.value = false
  intent.value = newIntent
  intentResolved.value = false
  await submitIntent(newIntent)
}

async function recoverUnknownCommand() {
  if (!canRecoverIntent.value || intent.value === null) return
  await submitIntent(intent.value)
}

async function submitIntent(selectedIntent: CommandIntent) {
  if (submitting.value) return
  submitting.value = true
  const controller = new AbortController()
  submissionController = controller
  const sequenceDeviceId = props.deviceId
  try {
    const snapshot = await createCommand(selectedIntent.deviceId, selectedIntent.idempotencyKey, controller.signal)
    if (sequenceDeviceId !== props.deviceId || controller.signal.aborted) return
    const resolvedIntent = { ...selectedIntent, commandId: snapshot.id }
    intent.value = resolvedIntent
    if (!writeCommandIntent(resolvedIntent)) {
      submissionError.value = 'The command was stored, but its recovery record could not be updated. Keep this page open and check the command result.'
    }
    submissionUnknown.value = false
    selectedCommandError.value = ''
    selectCommand(snapshot)
    const rowIndex = commandRows.value.findIndex(command => command.id === snapshot.id)
    if (rowIndex === -1) commandRows.value = [snapshot, ...commandRows.value].slice(0, DEFAULT_COMMAND_PAGE_SIZE)
    else commandRows.value[rowIndex] = snapshot
    void loadCommandHistory()
    if (isTerminalCommand(snapshot.status)) resolveIntent(snapshot)
    else if (trackingState.value !== 'tracking') startTracking()
  }
  catch (error) {
    if (controller.signal.aborted || sequenceDeviceId !== props.deviceId) return
    submissionError.value = safeSubmissionError(error)
    submissionUnknown.value = !(error instanceof GraphQLClientError && error.code === 'BAD_USER_INPUT')
    if (!submissionUnknown.value && removeCommandIntent(selectedIntent)) {
      intent.value = null
      submissionError.value += ' Review the device, then confirm a new PING if needed.'
    }
    else if (!submissionUnknown.value) {
      recoveryIssue.value = 'unavailable'
      submissionError.value += ' Browser recovery data could not be cleared, so new commands remain disabled.'
    }
  }
  finally {
    if (submissionController === controller) {
      submissionController = undefined
      submitting.value = false
    }
  }
}

function refreshSelectedCommand() {
  if (selectedCommandId.value === null || selectedReadPending.value) return
  stopTracking()
  abortDetailRead()
  void readSelectedCommand()
}

function resumeTracking() {
  if (selectedCommand.value === null || isTerminalCommand(selectedCommand.value.status) || selectedReadPending.value) return
  selectedCommandError.value = ''
  startTracking()
}

function retryIntentCleanup() {
  if (intent.value === null || !intentResolved.value || !removeCommandIntent(intent.value)) return
  intent.value = null
  intentResolved.value = false
  intentCleanupError.value = false
  recoveryIssue.value = null
  submissionUnknown.value = false
  submissionError.value = ''
}

function retryRecoveryRead() {
  if (recoveryIssue.value === 'unavailable') {
    hydrateIntentForDevice()
    if (recoveryIssue.value === null && selectedCommandId.value !== null) refreshSelectedCommand()
    return
  }
  refreshSelectedCommand()
}

function forgetUnusableRecovery() {
  if (!confirmingForget.value) {
    confirmingForget.value = true
    return
  }
  if (!forgetUnusableCommandIntent(props.deviceId)) {
    recoveryIssue.value = 'unavailable'
    submissionError.value = 'The saved recovery data could not be removed. No new command was sent.'
    return
  }
  confirmingForget.value = false
  recoveryIssue.value = null
  submissionError.value = ''
  submissionUnknown.value = false
}

const confirmingForget = ref(false)

function handleVisibilityChange() {
  if (document.visibilityState === 'hidden') {
    if (trackingState.value === 'tracking') {
      visibilityPaused = true
      clearTrackingTimers()
      if (currentDetailIsAutomatic) abortDetailRead()
    }
    if (selectedReadPending.value) abortDetailRead()
    historyController?.abort()
    return
  }
  if (!visibilityPaused) return
  visibilityPaused = false
  if (trackingState.value !== 'tracking' || selectedCommand.value === null || isTerminalCommand(selectedCommand.value.status)) return
  if (trackingBudgetExpired()) {
    pauseForBudget()
    return
  }
  armTrackingDeadline()
  void readSelectedCommand(true)
}

watch(() => props.deviceId, () => {
  if (import.meta.client) initializePanel()
})

onMounted(() => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  initializePanel()
})

onBeforeUnmount(() => {
  stopTracking()
  historySequence += 1
  historyController?.abort()
  abortDetailRead()
  submissionController?.abort()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<template>
  <section
    class="command-panel"
    aria-labelledby="command-heading"
    :aria-busy="historyState === 'loading' || submitting || selectedReadPending"
  >
    <div class="page-toolbar command-toolbar">
      <div>
        <p class="eyebrow">
          Device control
        </p>
        <h2
          id="command-heading"
          class="section-heading"
        >
          Diagnostic command
        </h2>
        <p class="page-intro">
          Send a parameterless PING to {{ deviceKey }} and inspect its server-recorded result.
        </p>
      </div>
      <div class="command-toolbar-actions">
        <button
          class="secondary-action"
          type="button"
          :disabled="historyRefreshing || historyState === 'loading'"
          :aria-busy="historyRefreshing"
          @click="loadCommandHistory()"
        >
          {{ historyRefreshing ? 'Refreshing history…' : 'Refresh command history' }}
        </button>
        <button
          v-if="!confirming && canStartNewIntent"
          class="primary-action"
          type="button"
          @click="confirming = true; submissionError = ''; submissionUnknown = false"
        >
          Send PING
        </button>
      </div>
    </div>

    <div
      v-if="confirming"
      class="command-confirmation"
      aria-labelledby="command-confirmation-heading"
    >
      <div>
        <h3 id="command-confirmation-heading">
          Confirm diagnostic PING
        </h3>
        <p>PulseGrid will send a parameterless diagnostic request to {{ deviceKey }}. Acceptance means the intent was stored; it does not mean the device has completed it.</p>
      </div>
      <div class="command-actions">
        <button
          class="secondary-action"
          type="button"
          :disabled="submitting"
          @click="confirming = false"
        >
          Cancel
        </button>
        <button
          class="primary-action"
          type="button"
          :disabled="submitting || !canStartNewIntent"
          @click="confirmNewCommand"
        >
          {{ submitting ? 'Sending PING…' : 'Confirm and send PING' }}
        </button>
      </div>
    </div>

    <div
      v-if="recoveryIssue !== null"
      class="command-alert"
      role="alert"
    >
      <div>
        <strong>{{ recoveryIssue === 'invalid' ? 'Saved command recovery data is invalid.' : 'Command recovery storage is unavailable.' }}</strong>
        <p>Command history remains readable. A new PING is disabled until this recovery state is reviewed because its previous outcome cannot be confirmed here.</p>
        <div class="command-actions">
          <button
            class="text-action"
            type="button"
            @click="retryRecoveryRead"
          >
            Check recovery data again
          </button>
          <button
            v-if="recoveryIssue === 'invalid'"
            class="text-action"
            type="button"
            @click="forgetUnusableRecovery"
          >
            {{ confirmingForget ? 'Confirm clearing saved recovery data' : 'Forget unusable recovery data' }}
          </button>
        </div>
        <p
          v-if="confirmingForget"
          class="command-warning"
        >
          Review the recent command history first. Clearing this record may leave an earlier PING's outcome unknown.
        </p>
      </div>
    </div>

    <div
      v-if="intent !== null && intent.commandId === null"
      class="command-alert command-alert--information"
      role="status"
      aria-live="polite"
    >
      <div>
        <strong>{{ submissionUnknown ? 'Submission outcome is unknown.' : 'This submission needs recovery.' }}</strong>
        <p>Recovery reuses the saved key for the same device and PING. It will not create a new logical command if the original intent was stored.</p>
        <button
          class="secondary-action"
          type="button"
          :disabled="!canRecoverIntent"
          @click="recoverUnknownCommand"
        >
          {{ submitting ? 'Recovering…' : 'Recover this submission' }}
        </button>
      </div>
    </div>

    <div
      v-if="submissionError"
      class="command-alert"
      role="alert"
    >
      <span>{{ submissionError }}</span>
    </div>

    <div
      v-if="intentCleanupError"
      class="command-alert"
      role="alert"
    >
      The terminal result is stored by the server. Clear its local recovery record before starting another PING.
      <button
        class="text-action"
        type="button"
        @click="retryIntentCleanup"
      >
        Retry recovery cleanup
      </button>
    </div>

    <p
      v-if="submitting"
      class="command-progress"
      role="status"
      aria-live="polite"
    >
      {{ intent?.commandId === null ? 'Sending the command intent…' : 'Recovering the saved command intent…' }}
    </p>

    <div
      v-if="historyError"
      class="command-alert"
      role="alert"
    >
      <span>{{ historyError }} The selected command remains available if it was already loaded.</span>
      <button
        class="text-action"
        type="button"
        @click="loadCommandHistory()"
      >
        Retry history
      </button>
    </div>

    <div
      v-if="historyState === 'loading'"
      class="command-empty"
      role="status"
      aria-live="polite"
    >
      Loading command history…
    </div>

    <div
      v-if="selectedCommandId !== null && selectedCommand === null && selectedCommandMissing"
      class="command-alert"
      role="alert"
    >
      <span>{{ selectedCommandError }}</span>
      <button
        class="text-action"
        type="button"
        :disabled="selectedReadPending"
        @click="refreshSelectedCommand"
      >
        Retry command lookup
      </button>
    </div>
    <div
      v-else-if="selectedCommandId !== null && selectedCommand === null && selectedReadPending"
      class="command-empty"
      role="status"
      aria-live="polite"
    >
      Checking the saved command status…
    </div>
    <div
      v-else-if="selectedCommandId !== null && selectedCommand === null && selectedCommandError"
      class="command-alert"
      role="alert"
    >
      <span>{{ selectedCommandError }}</span>
      <button
        class="text-action"
        type="button"
        :disabled="selectedReadPending"
        @click="refreshSelectedCommand"
      >
        Retry command lookup
      </button>
    </div>

    <template v-if="historyState === 'ready' || selectedCommand !== null">
      <div
        v-if="selectedCommand === null && !selectedCommandMissing && intent?.commandId === null"
        class="command-empty"
        role="status"
      >
        No recent commands are available for this device. The saved intent can still be recovered above.
      </div>
      <div
        v-else-if="selectedCommand === null && !selectedCommandMissing && commandRows.length === 0"
        class="command-empty"
        role="status"
      >
        No command has been sent to this device yet.
      </div>
      <article
        v-if="selectedCommand"
        class="command-current"
        aria-labelledby="selected-command-heading"
        :aria-busy="selectedReadPending"
      >
        <div class="command-current-heading">
          <div>
            <p class="eyebrow">
              Selected command
            </p>
            <h3 id="selected-command-heading">
              PING
            </h3>
            <code class="command-id">{{ formatCommandId(selectedCommand.id) }}</code>
          </div>
          <span
            class="command-status"
            :class="statusClass(selectedCommand.status)"
          >
            {{ statusLabels[selectedCommand.status] }}
          </span>
        </div>
        <p
          class="command-status-description"
          aria-live="polite"
        >
          {{ statusDescription(selectedCommand.status) }}
        </p>
        <p
          v-if="selectedCommandError"
          class="command-alert command-alert--inline"
          role="alert"
        >
          {{ selectedCommandError }}
        </p>
        <p
          v-if="selectedReadAt !== null"
          class="command-check-time"
        >
          Last confirmed {{ formatDate(new Date(selectedReadAt).toISOString()) }}
          <span v-if="selectedReadPending"> · Refreshing…</span>
        </p>
        <p
          v-if="selectedReadPending && selectedReadAt === null"
          class="command-check-time"
          role="status"
        >
          Checking stored command status…
        </p>
        <dl class="command-milestones">
          <div>
            <dt>Created</dt>
            <dd>{{ formatDate(selectedCommand.createdAt) }}</dd>
          </div>
          <div>
            <dt>Expires</dt>
            <dd>{{ formatDate(selectedCommand.expiresAt) }}</dd>
          </div>
          <div>
            <dt>Dispatched</dt>
            <dd>{{ formatDate(selectedCommand.dispatchedAt) }}</dd>
          </div>
          <div>
            <dt>Acknowledged</dt>
            <dd>{{ formatDate(selectedCommand.acknowledgedAt) }}</dd>
          </div>
          <div>
            <dt>Terminal result</dt>
            <dd>{{ formatDate(selectedCommand.terminalAt) }}</dd>
          </div>
          <div v-if="selectedCommand.failureCode">
            <dt>Failure reason</dt>
            <dd>{{ selectedCommand.failureCode === 'DEVICE_REPORTED_FAILURE' ? 'Reported by the device' : 'Delivery failed' }}</dd>
          </div>
        </dl>
        <p
          v-if="selectedCommand.status === 'TIMED_OUT'"
          class="command-timeout-note"
        >
          A timeout records the absence of a confirmed terminal response; it does not prove the device had no effect.
        </p>
        <div class="command-actions command-current-actions">
          <button
            class="secondary-action"
            type="button"
            :disabled="selectedReadPending"
            @click="refreshSelectedCommand"
          >
            Refresh command status
          </button>
          <button
            v-if="!terminal && trackingState !== 'tracking'"
            class="text-action"
            type="button"
            :disabled="selectedReadPending"
            @click="resumeTracking"
          >
            Resume status tracking
          </button>
        </div>
        <p
          v-if="trackingMessage"
          class="command-tracking-message"
          role="status"
          aria-live="polite"
        >
          {{ trackingMessage }}
        </p>
      </article>

      <section
        v-if="historyState === 'ready'"
        class="command-history"
        aria-labelledby="command-history-heading"
      >
        <div class="command-history-heading">
          <div>
            <p class="eyebrow">
              Recent activity
            </p>
            <h3 id="command-history-heading">
              Command history
            </h3>
          </div>
          <p>{{ historyCountDescription }}</p>
        </div>
        <p
          v-if="commandRows.length === 0"
          class="command-empty"
        >
          No commands are recorded in recent history.
        </p>
        <ol
          v-else
          class="command-history-list"
          aria-label="Recent commands, newest first"
        >
          <li
            v-for="command in commandRows"
            :key="command.id"
          >
            <button
              class="command-history-row"
              type="button"
              :aria-pressed="selectedCommandId === command.id"
              @click="selectCommand(command)"
            >
              <span class="command-history-main">
                <span class="command-history-title">PING · {{ formatDate(command.createdAt) }}</span>
                <code>{{ command.id }}</code>
              </span>
              <span
                class="command-status"
                :class="statusClass(command.status)"
              >
                {{ statusLabels[command.status] }}
              </span>
            </button>
          </li>
        </ol>
        <p
          v-if="!selectedCommand && commandRows.length > 0"
          class="command-history-hint"
        >
          Select a command to refresh its stored status.
        </p>
      </section>
    </template>
  </section>
</template>

<style scoped>
.command-panel {
  margin-top: 48px;
}

.command-toolbar {
  align-items: flex-end;
}

.command-toolbar-actions,
.command-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}

.command-toolbar-actions {
  justify-content: flex-end;
}

.command-confirmation,
.command-current,
.command-history,
.command-empty {
  margin-top: 24px;
  border: 1px solid var(--pulse-border);
  border-radius: 14px;
  background: var(--pulse-surface);
  box-shadow: var(--pulse-shadow-card);
  padding: clamp(18px, 3vw, 28px);
}

.command-confirmation {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  border-color: color-mix(in srgb, var(--pulse-focus) 55%, var(--pulse-border));
  background: var(--pulse-surface-secondary);
}

.command-confirmation h3,
.command-current h3,
.command-history h3 {
  margin: 6px 0 0;
  color: var(--pulse-text-primary);
  font-size: 20px;
  font-weight: 750;
}

.command-confirmation p {
  max-width: 680px;
  margin: 8px 0 0;
  color: var(--pulse-text-secondary);
  line-height: 1.55;
}

.command-alert {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  margin-top: 20px;
  border: 1px solid color-mix(in srgb, #b25243 42%, var(--pulse-border));
  border-radius: 12px;
  background: #fff7f4;
  color: #8b3e32;
  padding: 14px 16px;
  line-height: 1.55;
}

.command-alert--information {
  border-color: color-mix(in srgb, #4d7fc1 35%, var(--pulse-border));
  background: #f1f6fc;
  color: #315681;
}

.command-alert--inline {
  justify-content: flex-start;
  margin-top: 14px;
}

.command-alert p {
  margin: 6px 0 12px;
  color: inherit;
}

.command-warning {
  font-size: 13px;
  font-weight: 650;
}

.command-progress,
.command-check-time,
.command-tracking-message,
.command-history-heading > p,
.command-history-hint {
  color: var(--pulse-text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.command-empty {
  color: var(--pulse-text-secondary);
  line-height: 1.55;
}

.command-current-heading,
.command-history-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}

.command-id,
.command-history-row code {
  display: block;
  margin-top: 10px;
  color: var(--pulse-text-secondary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.command-status {
  display: inline-flex;
  min-height: 30px;
  align-items: center;
  border: 1px solid transparent;
  border-radius: 999px;
  padding: 4px 11px;
  font-size: 13px;
  font-weight: 750;
  white-space: nowrap;
}

.command-status--success {
  border-color: #c4e8d9;
  background: #ddf2e8;
  color: #246b50;
}

.command-status--failure,
.command-status--timeout {
  border-color: #efc7c3;
  background: #f8dedc;
  color: #8b3e32;
}

.command-status--pending,
.command-status--dispatched,
.command-status--acknowledged {
  border-color: var(--pulse-border);
  background: var(--pulse-surface-secondary);
  color: var(--pulse-text-secondary);
}

.command-status-description {
  margin: 16px 0 0;
  color: var(--pulse-text-secondary);
  line-height: 1.55;
}

.command-milestones {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px 20px;
  margin: 22px 0 0;
}

.command-milestones div {
  min-width: 0;
}

.command-milestones dt {
  color: var(--pulse-text-secondary);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
}

.command-milestones dd {
  margin: 6px 0 0;
  color: var(--pulse-text-primary);
  font-size: 14px;
  overflow-wrap: anywhere;
}

.command-timeout-note {
  margin: 18px 0 0;
  border-left: 3px solid #d99a32;
  color: var(--pulse-text-secondary);
  padding-left: 12px;
  line-height: 1.55;
}

.command-current-actions {
  margin-top: 20px;
}

.command-history-heading {
  align-items: flex-end;
}

.command-history-heading > p {
  margin: 0;
  text-align: right;
}

.command-history-list {
  display: grid;
  gap: 10px;
  margin: 18px 0 0;
  padding: 0;
  list-style: none;
}

.command-history-row {
  display: flex;
  width: 100%;
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border: 1px solid var(--pulse-border);
  border-radius: 10px;
  background: var(--pulse-surface-elevated);
  padding: 12px 14px;
  text-align: left;
}

.command-history-row[aria-pressed='true'] {
  border-color: var(--pulse-primary);
  background: var(--pulse-primary-soft);
}

.command-history-main {
  min-width: 0;
}

.command-history-title {
  color: var(--pulse-text-primary);
  font-size: 14px;
  font-weight: 750;
}

.command-history-row code {
  margin-top: 4px;
}

.command-history-hint {
  margin: 12px 0 0;
}

@media (max-width: 767px) {
  .command-toolbar-actions,
  .command-actions {
    width: 100%;
  }

  .command-toolbar-actions > *,
  .command-actions > * {
    flex: 1;
  }

  .command-confirmation,
  .command-current-heading,
  .command-history-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .command-milestones {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .command-history-heading > p {
    text-align: left;
  }
}
</style>
