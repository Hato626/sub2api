<template>
  <BaseDialog :show="show" :title="t('admin.accounts.bulkTest.title')" width="wide" @close="handleClose">
    <div class="space-y-4">
      <div class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-500 dark:bg-dark-700">
        <div class="flex flex-wrap items-end gap-3">
          <label class="min-w-[220px] flex-1 text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t('admin.accounts.bulkTest.modeLabel') }}
            <select
              v-model="mode"
              :disabled="running"
              class="mt-1 block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-dark-500 dark:bg-dark-800 dark:text-gray-100"
            >
              <option value="standard">{{ t('admin.accounts.diagnosticTestModeStandard') }}</option>
              <option value="pelican">{{ t('admin.accounts.diagnosticTestModePelican') }}</option>
              <option value="attribution">{{ t('admin.accounts.diagnosticTestModeAttribution') }}</option>
            </select>
          </label>
          <div class="flex-1 text-xs text-gray-500 dark:text-gray-400">
            {{ mode === 'attribution' ? t('admin.accounts.bulkTest.attributionHint') : mode === 'pelican' ? t('admin.accounts.bulkTest.pelicanHint') : t('admin.accounts.bulkTest.standardHint') }}
          </div>
        </div>
        <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
          <span>{{ t('admin.accounts.bulkTest.parallelHint') }}</span>
          <span>{{ t('admin.accounts.bulkTest.progress', { completed: completedCount, total: rows.length }) }}</span>
        </div>
      </div>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300">
        {{ errorMessage }}
      </div>

      <div class="max-h-[min(52vh,560px)] space-y-2 overflow-y-auto pr-1">
        <div
          v-for="row in rows"
          :key="row.id"
          class="rounded-lg border p-3 dark:border-dark-500"
          :class="row.status === 'error' ? 'border-red-200 bg-red-50/60 dark:border-red-900/50 dark:bg-red-900/10' : 'border-gray-200 bg-white dark:bg-dark-800'"
        >
          <div class="flex items-center gap-2">
            <span class="w-8 text-xs font-mono text-gray-400">#{{ row.id }}</span>
            <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-800 dark:text-gray-100">{{ row.name }}</span>
            <span
              class="rounded-full px-2 py-0.5 text-xs font-medium"
              :class="statusClass(row.status)"
            >
              {{ statusLabel(row.status) }}
            </span>
          </div>
          <div v-if="row.message" class="mt-1 pl-10 text-xs text-gray-500 dark:text-gray-400">{{ row.message }}</div>
          <div v-if="row.prediction" class="mt-2 pl-10 text-xs text-primary-700 dark:text-primary-300">
            {{ t('admin.accounts.bulkTest.prediction', { model: row.prediction, probability: row.probability }) }}
          </div>
          <PelicanPreview v-if="row.pelicanOutput" :source="row.pelicanOutput" compact class="mt-3" />
          <details v-else-if="row.output" class="mt-2 pl-10">
            <summary class="cursor-pointer text-xs text-primary-600 dark:text-primary-400">{{ t('admin.accounts.bulkTest.viewOutput') }}</summary>
            <pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-gray-900 p-2 text-xs text-gray-100">{{ row.output }}</pre>
          </details>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          class="rounded-lg bg-gray-100 px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
          :disabled="running"
          @click="handleClose"
        >
          {{ t('common.close') }}
        </button>
        <button
          v-if="running"
          class="rounded-lg bg-orange-500 px-4 py-2 text-sm font-medium text-white hover:bg-orange-600"
          @click="cancelRun"
        >
          {{ t('admin.accounts.bulkTest.cancel') }}
        </button>
        <button
          v-else
          class="rounded-lg bg-primary-500 px-4 py-2 text-sm font-medium text-white hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="rows.length === 0"
          @click="startRun"
        >
          {{ completedCount > 0 ? t('admin.accounts.bulkTest.retry') : t('admin.accounts.bulkTest.start') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PelicanPreview from './PelicanPreview.vue'
import { buildApiUrl } from '@/api/client'
import { ADMIN_UI_REQUEST_HEADER } from '@/api/adminUIRequest'
import { adminAPI } from '@/api/admin'
import {
  analyzeModelTrace,
  createModelTraceChallenges,
  loadModelTraceBank,
  PELICAN_PROMPT,
  type ModelTraceResult
} from '@/utils/modeltrace'

type BatchMode = 'standard' | 'pelican' | 'attribution'
type RowStatus = 'pending' | 'running' | 'success' | 'error' | 'cancelled'

interface AccountSummary {
  name?: string
  platform?: string
  type?: string
}

interface TestRow {
  id: number
  name: string
  status: RowStatus
  message: string
  output: string
  pelicanOutput: string
  prediction: string
  probability: string
}

interface TestEvent {
  type: string
  text?: string
  model?: string
  success?: boolean
  error?: string
}

const props = defineProps<{
  show: boolean
  accountIds: number[]
  accountSummaries?: Record<string, AccountSummary>
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'completed'): void
}>()

const { t } = useI18n()
const mode = ref<BatchMode>('standard')
const rows = ref<TestRow[]>([])
const running = ref(false)
const errorMessage = ref('')
const activeControllers = new Set<AbortController>()
const MAX_PARALLEL_TESTS = 4
const completedCount = computed(() => rows.value.filter((row) => ['success', 'error', 'cancelled'].includes(row.status)).length)

const createRows = () => {
  rows.value = props.accountIds.map((id) => ({
    id,
    name: props.accountSummaries?.[String(id)]?.name || `#${id}`,
    status: 'pending',
    message: '',
    output: '',
    pelicanOutput: '',
    prediction: '',
    probability: ''
  }))
  errorMessage.value = ''
}

watch(() => [props.show, props.accountIds] as const, ([show]) => {
  if (show && !running.value) createRows()
}, { immediate: true })

const statusClass = (status: RowStatus) => {
  if (status === 'success') return 'bg-green-100 text-green-700 dark:bg-green-500/20 dark:text-green-300'
  if (status === 'error') return 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-300'
  if (status === 'running') return 'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300'
  if (status === 'cancelled') return 'bg-gray-200 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
  return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
}

const statusLabel = (status: RowStatus) => t(`admin.accounts.bulkTest.status.${status}`)

const parseSSE = async (accountID: number, prompt: string, platform: string): Promise<string> => {
  const controller = new AbortController()
  activeControllers.add(controller)
  const body: { model_id: string; prompt: string; mode?: string } = { model_id: '', prompt }
  if (platform === 'grok') body.mode = 'text'
  if (platform === 'openai') body.mode = 'default'

  try {
    const response = await fetch(buildApiUrl(`/admin/accounts/${accountID}/test`), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
        'Content-Type': 'application/json',
        [ADMIN_UI_REQUEST_HEADER]: '1'
      },
      body: JSON.stringify(body),
      signal: controller.signal
    })
    if (!response.ok) {
      const detail = (await response.text()).trim()
      throw new Error(detail ? `HTTP ${response.status}: ${detail.slice(0, 300)}` : `HTTP ${response.status}`)
    }
    const reader = response.body?.getReader()
    if (!reader) throw new Error(t('admin.accounts.bulkTest.noResponseBody'))
    const decoder = new TextDecoder()
    let buffer = ''
    let output = ''
    let completed = false
    const consumeLine = (line: string) => {
      if (!line.startsWith('data: ')) return
      const payload = line.slice(6).trim()
      if (!payload) return
      const event = JSON.parse(payload) as TestEvent
      if (event.type === 'content' && event.text) output += event.text
      if (event.type === 'error') throw new Error(event.error || t('admin.accounts.bulkTest.failed'))
      if (event.type === 'test_complete') {
        if (!event.success) throw new Error(event.error || t('admin.accounts.bulkTest.failed'))
        completed = true
      }
    }
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      lines.forEach(consumeLine)
    }
    if (buffer) consumeLine(buffer)
    if (!completed) throw new Error(t('admin.accounts.bulkTest.incomplete'))
    return output
  } finally {
    activeControllers.delete(controller)
  }
}

const runOne = async (row: TestRow) => {
  const account = await adminAPI.accounts.getById(row.id)
  if (!running.value) throw new DOMException('Batch test cancelled', 'AbortError')
  row.name = account.name || row.name
  row.status = 'running'
  row.message = t('admin.accounts.bulkTest.running')
  const prompts = mode.value === 'pelican' ? [{ prompt: PELICAN_PROMPT, expectedCount: 0 }] : mode.value === 'attribution' ? createModelTraceChallenges(3) : [{ prompt: '', expectedCount: 0 }]
  const outputs: Array<{ text: string; expectedCount: number }> = []
  for (const challenge of prompts) {
    const output = await parseSSE(account.id, challenge.prompt, account.platform)
    if (!output.trim()) throw new Error(t('admin.accounts.bulkTest.emptyResponse'))
    outputs.push({ text: output, expectedCount: challenge.expectedCount })
    if (mode.value === 'pelican') row.pelicanOutput = output
    else row.output = output.slice(0, 5000)
  }
  if (mode.value === 'attribution') {
    const result: ModelTraceResult = analyzeModelTrace(outputs, await loadModelTraceBank())
    row.prediction = result.predictionName
    row.probability = `${(result.probability * 100).toFixed(1)}%`
  }
  row.status = 'success'
  row.message = t('admin.accounts.bulkTest.success')
}

const startRun = async () => {
  if (running.value || rows.value.length === 0) return
  errorMessage.value = ''
  rows.value.forEach((row) => {
    row.status = 'pending'
    row.message = ''
    row.output = ''
    row.pelicanOutput = ''
    row.prediction = ''
    row.probability = ''
  })
  running.value = true
  try {
    const queue = [...rows.value]
    const worker = async () => {
      while (running.value) {
        const row = queue.shift()
        if (!row) return
        try {
          await runOne(row)
        } catch (error: unknown) {
          if (error instanceof DOMException && error.name === 'AbortError') {
            row.status = 'cancelled'
            row.message = t('admin.accounts.bulkTest.cancelled')
            return
          }
          row.status = 'error'
          row.message = error instanceof Error ? error.message : t('admin.accounts.bulkTest.failed')
        }
      }
    }
    await Promise.all(Array.from({ length: Math.min(MAX_PARALLEL_TESTS, queue.length) }, () => worker()))
  } finally {
    running.value = false
    emit('completed')
  }
}

const cancelRun = () => {
  running.value = false
  activeControllers.forEach((controller) => controller.abort())
  rows.value.forEach((row) => {
    if (row.status === 'pending') {
      row.status = 'cancelled'
      row.message = t('admin.accounts.bulkTest.cancelled')
    }
  })
}

const handleClose = () => {
  if (running.value) return
  emit('close')
}
</script>
