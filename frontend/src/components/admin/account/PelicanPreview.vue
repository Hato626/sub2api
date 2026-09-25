<template>
  <section class="space-y-2" data-testid="pelican-preview">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.accounts.pelicanPreview.title') }}</span>
      <div class="flex gap-2">
        <button v-if="document" type="button" class="btn btn-secondary btn-sm" :aria-pressed="expanded" @click="expanded = !expanded">
          {{ t(expanded ? 'admin.accounts.pelicanPreview.collapse' : 'admin.accounts.pelicanPreview.expand') }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(source)">
          {{ t('admin.accounts.pelicanPreview.copySource') }}
        </button>
      </div>
    </div>
    <iframe
      v-if="document"
      :srcdoc="document"
      :title="t('admin.accounts.pelicanPreview.title')"
      sandbox=""
      referrerpolicy="no-referrer"
      loading="lazy"
      class="w-full rounded-lg border border-gray-200 bg-white dark:border-dark-500"
      :class="expanded ? 'h-[640px]' : compact ? 'h-[300px]' : 'h-[420px]'"
    />
    <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.pelicanPreview.noMarkup') }}</p>
    <details>
      <summary class="cursor-pointer text-xs text-primary-600 dark:text-primary-400">{{ t('admin.accounts.pelicanPreview.source') }}</summary>
      <pre class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-900 p-3 text-xs text-gray-100">{{ source }}</pre>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import { buildPelicanPreview } from '@/utils/pelicanPreview'

const props = defineProps<{ source: string; compact?: boolean }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const expanded = ref(false)
const document = computed(() => buildPelicanPreview(props.source))
watch(() => props.source, () => { expanded.value = false })
</script>
