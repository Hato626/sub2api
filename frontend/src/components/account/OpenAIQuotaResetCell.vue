<template>
  <div v-if="visible" class="space-y-1">
    <!--
      Unified action row. Parents that already render their own "local query"
      affordance (e.g. AccountUsageCell's active-sampling refresh) pass it in
      via the #pre-actions slot so the user sees a single row of related
      buttons rather than two near-duplicate "查询" rows.

      The 5h / 7d window bars are deliberately NOT rendered here — the local
      active-sampling display (UsageProgressBar in AccountUsageCell) already
      owns that real estate. This cell is purely about the rate-limit reset
      credit: query its count, consume one if needed.
    -->
    <div class="flex flex-wrap items-center gap-1.5">
      <slot name="pre-actions" />

      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading || resetting"
        :title="countButtonTitle"
        @click="handleQuery()"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': loading }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {{ t('admin.accounts.openaiQuotaReset.count') }}<span v-if="data"> {{ availableResetCount }}</span>
      </button>

      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-orange-600 transition-colors hover:bg-orange-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-orange-400 dark:hover:bg-orange-900/30"
        :disabled="resetting || loading || !canReset"
        :title="resetButtonTitle"
        @click="openResetConfirm"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': resetting }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M20 12a8 8 0 11-2.343-5.657L20 8m0 0V4m0 4h-4"
          />
        </svg>
        {{ t('admin.accounts.openaiQuotaReset.reset') }}
      </button>

      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-emerald-600 transition-colors hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
        :disabled="loading || resetting || inviting"
        :title="inviteButtonTitle"
        @click="openInviteDialog"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-pulse': inviting }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M18 14v4m0 0v4m0-4h4m-4 0h-4M15 8a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0"
          />
        </svg>
        {{ t('admin.accounts.openaiQuotaReset.invite') }}<span v-if="remainingInvitesLabel"> {{ remainingInvitesLabel }}</span>
      </button>
    </div>

    <div v-if="primaryResetCreditExpiry" class="space-y-1">
      <div class="flex flex-wrap items-center gap-1">
        <span
          class="inline-flex max-w-full items-center rounded bg-gray-100 px-1.5 py-0.5 text-[10px] leading-4 text-gray-600 tabular-nums dark:bg-dark-800 dark:text-gray-300"
          :title="t('admin.accounts.openaiQuotaReset.expiresAtFull', { time: formatResetCreditExpiry(primaryResetCreditExpiry, 'full') })"
        >
          {{ t('admin.accounts.openaiQuotaReset.expiresAt', { time: formatResetCreditExpiry(primaryResetCreditExpiry, 'short') }) }}
        </span>
        <button
          v-if="hiddenResetCreditCount > 0"
          type="button"
          data-testid="reset-credit-expiry-toggle"
          class="inline-flex items-center rounded-full bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium leading-4 text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700"
          :aria-expanded="showResetCreditDetails"
          :aria-label="resetCreditDetailsToggleLabel"
          :title="resetCreditDetailsTitle"
          @click="toggleResetCreditDetails"
        >
          +{{ hiddenResetCreditCount }}
        </button>
      </div>

      <div
        v-if="showResetCreditDetails && resetCreditExpirations.length > 1"
        data-testid="reset-credit-expiry-details"
        class="inline-grid max-w-full gap-0.5 rounded border border-gray-200 bg-white px-1.5 py-1 text-[10px] leading-4 text-gray-600 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300"
      >
        <span class="sr-only">{{ t('admin.accounts.openaiQuotaReset.expirationDetails') }}</span>
        <span
          v-for="(expiresAt, index) in resetCreditExpirations"
          :key="`${expiresAt}-${index}`"
          class="flex min-w-0 items-center gap-1 tabular-nums"
          :title="t('admin.accounts.openaiQuotaReset.expiresAtFull', { time: formatResetCreditExpiry(expiresAt, 'full') })"
        >
          <span class="h-1 w-1 shrink-0 rounded-full bg-gray-400 dark:bg-dark-500" />
          <span class="truncate">{{ formatResetCreditExpiry(expiresAt, 'short') }}</span>
        </span>
      </div>
    </div>

    <!-- Error / success feedback -->
    <div
      v-if="error"
      class="text-[10px] text-red-600 dark:text-red-400"
      :title="error"
    >
      {{ truncatedError }}
    </div>
    <div
      v-else-if="resetWarning"
      class="text-[10px] text-amber-600 dark:text-amber-400"
    >
      {{ resetWarning }}
    </div>
    <div
      v-else-if="resetMessage"
      class="text-[10px] text-emerald-600 dark:text-emerald-400"
    >
      {{ resetMessage }}
    </div>

    <Teleport to="body">
      <div
        v-if="inviteDialogOpen"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4"
        @click.self="closeInviteDialog"
      >
        <div
          class="w-full max-w-md rounded-lg bg-white p-4 shadow-xl dark:bg-dark-800"
          @click.stop
          @mousedown.stop
        >
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
              {{ t('admin.accounts.openaiQuotaReset.inviteTitle') }}
            </h3>
            <button
              type="button"
              class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200"
              :title="t('common.close')"
              @click="closeInviteDialog"
            >
              <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          <div class="mb-3 grid grid-cols-2 rounded-md bg-gray-100 p-0.5 text-xs dark:bg-dark-700">
            <button
              type="button"
              class="rounded px-2 py-1.5 font-medium transition-colors"
              :class="inviteMode === 'pool' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500 dark:text-gray-300'"
              @click.stop="setInviteMode('pool')"
            >
              {{ t('admin.accounts.openaiQuotaReset.inviteModePool') }}
            </button>
            <button
              type="button"
              class="rounded px-2 py-1.5 font-medium transition-colors"
              :class="inviteMode === 'email' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500 dark:text-gray-300'"
              @click.stop="setInviteMode('email')"
            >
              {{ t('admin.accounts.openaiQuotaReset.inviteModeEmail') }}
            </button>
          </div>

          <div v-if="inviteMode === 'pool'" class="space-y-2">
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.openaiQuotaReset.targetAccount') }}
            </label>
            <select
              v-model.number="selectedTargetAccountID"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-100"
              :disabled="loadingAccounts || inviting"
            >
              <option :value="0">
                {{ loadingAccounts ? t('common.loading') : t('admin.accounts.openaiQuotaReset.selectTarget') }}
              </option>
              <option
                v-for="item in targetAccounts"
                :key="item.id"
                :value="item.id"
              >
                {{ accountOptionLabel(item) }}
              </option>
            </select>
          </div>

          <div v-else class="space-y-2">
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300">
              {{ t('admin.accounts.openaiQuotaReset.email') }}
            </label>
            <input
              v-model.trim="inviteEmail"
              type="email"
              class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-100"
              :placeholder="t('admin.accounts.openaiQuotaReset.emailPlaceholder')"
              :disabled="inviting"
            />
          </div>

          <div
            v-if="inviteError"
            class="mt-3 rounded-md bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300"
          >
            {{ inviteError }}
          </div>

          <div
            v-if="inviteResult"
            class="mt-3 space-y-2 rounded-md bg-emerald-50 px-3 py-2 text-xs text-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-200"
          >
            <div>{{ inviteResultSummary }}</div>
            <a
              v-if="firstInviteURL"
              :href="firstInviteURL"
              target="_blank"
              rel="noreferrer"
              class="block truncate underline"
            >
              {{ firstInviteURL }}
            </a>
            <div v-if="autoRedeemSummary" class="text-emerald-700 dark:text-emerald-300">
              {{ autoRedeemSummary }}
            </div>
          </div>

          <div class="mt-4 flex justify-end gap-2">
            <button
              type="button"
              class="rounded-md px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              @click="closeInviteDialog"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              type="button"
              class="inline-flex items-center gap-1 rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-emerald-700 disabled:cursor-not-allowed disabled:opacity-50"
              :disabled="inviting || !canSubmitInvite"
              @click="handleSendInvite"
            >
              <svg
                class="h-3.5 w-3.5"
                :class="{ 'animate-spin': inviting }"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M22 2L11 13" />
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M22 2l-7 20-4-9-9-4 20-7z" />
              </svg>
              {{ t('admin.accounts.openaiQuotaReset.sendInvite') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <ConfirmDialog
      :show="showResetConfirm"
      :title="t('admin.accounts.openaiQuotaReset.confirmTitle')"
      :message="t('admin.accounts.openaiQuotaReset.confirmMessage', { count: availableResetCount })"
      :confirm-text="t('admin.accounts.openaiQuotaReset.reset')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmReset"
      @cancel="showResetConfirm = false"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import {
  list as listAccounts,
  queryOpenAIReferralStatus,
  refreshOpenAIQuota,
  resetOpenAIQuota,
  sendOpenAIReferralInvite,
  type OpenAIQuotaUsage,
  type OpenAIQuotaResetResult,
  type OpenAIReferralInviteResult,
  type OpenAIReferralStatus
} from '@/api/admin/accounts'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const props = defineProps<{
  account: Account
}>()

const emit = defineEmits<{
  'account-updated': [account: Account]
}>()

const { t } = useI18n()

// Visible only for OpenAI OAuth accounts.
const visible = computed(() => props.account.platform === 'openai' && props.account.type === 'oauth')

const loading = ref(false)
const resetting = ref(false)
const inviting = ref(false)
const loadingAccounts = ref(false)
const error = ref<string | null>(null)
const data = ref<OpenAIQuotaUsage | null>(null)
const referralStatus = ref<OpenAIReferralStatus | null>(null)
const cachedData = ref<OpenAIQuotaUsage | null>(null)
const resetMessage = ref<string | null>(null)
const resetWarning = ref<string | null>(null)
const inviteDialogOpen = ref(false)
const inviteMode = ref<'pool' | 'email'>('pool')
const targetAccounts = ref<Account[]>([])
const selectedTargetAccountID = ref<number>(0)
const inviteEmail = ref('')
const inviteError = ref<string | null>(null)
const inviteResult = ref<OpenAIReferralInviteResult | null>(null)
const showResetConfirm = ref(false)
const showResetCreditDetails = ref(false)

// Rehydrate the card from the persisted snapshot. Credits that already expired
// are dropped and the count is clamped to what remains: the snapshot has no
// freshness signal, so an unfiltered read would offer to consume credits that no
// longer exist. A snapshot claiming credits with no usable expiration left is
// treated as absent, which keeps the reset button gated on a live query.
const readCachedResetCredits = (account: Account): OpenAIQuotaUsage | null => {
  const cached = account.extra?.codex_reset_credit_snapshot
  if (!cached || typeof cached !== 'object' || Array.isArray(cached)) return null

  const { available_count: count, credits: rawCredits } = cached as {
    available_count?: unknown
    credits?: unknown
  }
  if (typeof count !== 'number' || !Number.isFinite(count)) return null

  const now = Date.now()
  const credits: { expires_at?: string }[] = []
  if (Array.isArray(rawCredits)) {
    for (const credit of rawCredits) {
      if (!credit || typeof credit !== 'object') continue
      const expiresAt = (credit as { expires_at?: unknown }).expires_at
      if (typeof expiresAt !== 'string' || expiresAt.trim() === '') continue
      const expiryTime = new Date(expiresAt).getTime()
      // Unparsable timestamps are kept: they are already rendered verbatim and
      // dropping them would silently understate the available count.
      if (!Number.isNaN(expiryTime) && expiryTime <= now) continue
      credits.push({ expires_at: expiresAt })
    }
  }
  const availableCount = Math.min(Math.max(count, 0), credits.length)
  // A snapshot that claimed credits but has none left is no longer informative;
  // report "unknown" so the operator re-queries instead of trusting it.
  if (count > 0 && availableCount <= 0) return null
  return {
    fetched_at: 0,
    rate_limit_reset_credits: {
      available_count: availableCount,
      credits
    }
  }
}

cachedData.value = readCachedResetCredits(props.account)
data.value = cachedData.value

// 影子账号的额度查询会 resolve 到母账号,但影子本身不支持重置(后端返回 409);
// 重置必须在母账号上进行。前端据此禁用影子的重置入口(外审 F6)。
const isShadow = computed(() => props.account.parent_account_id != null)

const availableResetCount = computed(() => data.value?.rate_limit_reset_credits?.available_count ?? 0)
// Prefer the live payload and fall back to the persisted snapshot only when the
// live state is unknown, so the count and the expirations never come from two
// different generations of the same data.
const resetCreditExpirations = computed(() =>
  ((data.value ?? cachedData.value)?.rate_limit_reset_credits?.credits ?? [])
    .map((credit) => credit.expires_at?.trim() ?? '')
    .filter((expiresAt) => expiresAt.length > 0)
    .sort(compareResetCreditExpiry)
)
const primaryResetCreditExpiry = computed(() => resetCreditExpirations.value[0] ?? '')
const hiddenResetCreditCount = computed(() => Math.max(resetCreditExpirations.value.length - 1, 0))
const canReset = computed(() => availableResetCount.value > 0 && !isShadow.value)
const remainingInvitesLabel = computed(() => {
  const count = referralStatus.value?.remaining_invites
  return typeof count === 'number' ? String(count) : ''
})

const resetCreditDetailsTitle = computed(() =>
  resetCreditExpirations.value
    .map((expiresAt) => formatResetCreditExpiry(expiresAt, 'full'))
    .join('\n')
)

const resetCreditDetailsToggleLabel = computed(() => {
  if (showResetCreditDetails.value) {
    return t('admin.accounts.openaiQuotaReset.collapseExpirations')
  }
  return t('admin.accounts.openaiQuotaReset.expandExpirations', { count: hiddenResetCreditCount.value })
})

const resetButtonTitle = computed(() => {
  if (isShadow.value) return t('admin.accounts.openaiQuotaReset.resetTooltipShadow')
  if (!data.value) return t('admin.accounts.openaiQuotaReset.resetTooltipNeedQuery')
  if (!canReset.value) return t('admin.accounts.openaiQuotaReset.resetTooltipNoCredits')
  return t('admin.accounts.openaiQuotaReset.resetTooltipReady')
})

// "次数" button doubles as the upstream-query trigger and the count display.
// Tooltip differs between "click to load" (no data yet) and "click to refresh".
const countButtonTitle = computed(() => {
  if (!data.value) return t('admin.accounts.openaiQuotaReset.countTooltipLoad')
  return t('admin.accounts.openaiQuotaReset.countTooltipRefresh')
})

const inviteButtonTitle = computed(() => {
  if (!referralStatus.value) return t('admin.accounts.openaiQuotaReset.inviteTooltipLoad')
  if (typeof referralStatus.value.remaining_invites === 'number') {
    return t('admin.accounts.openaiQuotaReset.inviteTooltipWithCount', {
      count: referralStatus.value.remaining_invites
    })
  }
  return t('admin.accounts.openaiQuotaReset.inviteTooltipReady')
})

const canSubmitInvite = computed(() => {
  if (inviteMode.value === 'pool') return selectedTargetAccountID.value > 0
  return inviteEmail.value.trim().length > 0
})

const firstInviteURL = computed(() => {
  const invite = inviteResult.value?.invites?.find((item) => item.invite_url)
  return invite?.invite_url || ''
})

const inviteResultSummary = computed(() => {
  if (!inviteResult.value) return ''
  const emails = inviteResult.value.emails?.join(', ') || ''
  return t('admin.accounts.openaiQuotaReset.inviteSuccess', { emails })
})

const autoRedeemSummary = computed(() => {
  const auto = inviteResult.value?.auto_redeem
  if (!auto) return ''
  if (!auto.attempted) return t('admin.accounts.openaiQuotaReset.autoRedeemSkipped', { reason: auto.reason || '-' })
  if (auto.verified) return t('admin.accounts.openaiQuotaReset.autoRedeemVerified')
  if (auto.success) return t('admin.accounts.openaiQuotaReset.autoRedeemAttempted', { reason: auto.reason || '-' })
  return t('admin.accounts.openaiQuotaReset.autoRedeemFailed', { reason: auto.reason || '-' })
})

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}…` : error.value
})

const getResetCreditExpiryTime = (value: string): number => {
  const time = new Date(value).getTime()
  return Number.isNaN(time) ? Number.POSITIVE_INFINITY : time
}

const compareResetCreditExpiry = (a: string, b: string): number => {
  const diff = getResetCreditExpiryTime(a) - getResetCreditExpiryTime(b)
  if (diff !== 0) return diff
  return a.localeCompare(b)
}

const formatResetCreditExpiry = (value: string, style: 'short' | 'full'): string => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  const options: Intl.DateTimeFormatOptions = {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }
  if (style === 'full') {
    options.year = 'numeric'
  }

  return new Intl.DateTimeFormat(undefined, options).format(date)
}

const extractErrorMessage = (e: unknown): string => {
  // The project's axios response interceptor (api/client.ts) flattens server
  // errors into { status, code, message, reason, ... } and re-rejects them, so
  // the message lives at the top level rather than under .response.data. Fall
  // back to the raw axios shape for the cancellation/network branches that
  // bypass the flattening, and finally to the generic i18n string.
  const err = e as {
    message?: string
    reason?: string
    response?: { data?: { message?: string; error?: string } }
  }
  return (
    err?.message ||
    err?.reason ||
    err?.response?.data?.message ||
    err?.response?.data?.error ||
    t('common.error')
  )
}

const toggleResetCreditDetails = () => {
  if (hiddenResetCreditCount.value <= 0) return
  showResetCreditDetails.value = !showResetCreditDetails.value
}

const handleQuery = async () => {
  if (loading.value) return
  loading.value = true
  error.value = null
  resetMessage.value = null
  resetWarning.value = null
  showResetCreditDetails.value = false
  try {
    const [result, status] = await Promise.all([
      refreshOpenAIQuota(props.account.id),
      queryOpenAIReferralStatus(props.account.id).catch(() => null)
    ])
    data.value = result
    referralStatus.value = status
    // The upstream read succeeded even when the snapshot write was rejected, so
    // the live count is always adopted. Only the persisted view is left alone,
    // which keeps the displayed expirations consistent with what is stored.
    if (result.cache_persisted) {
      cachedData.value = result
    } else {
      resetWarning.value = t('admin.accounts.openaiQuotaReset.refreshCachePersistFailed')
    }
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    loading.value = false
  }
}

const openResetConfirm = () => {
  if (resetting.value || loading.value) return
  if (!canReset.value) {
    error.value = t('admin.accounts.openaiQuotaReset.noCreditsAvailable')
    return
  }
  showResetConfirm.value = true
}

const confirmReset = async () => {
  showResetConfirm.value = false
  if (resetting.value) return
  if (!canReset.value) {
    error.value = t('admin.accounts.openaiQuotaReset.noCreditsAvailable')
    return
  }
  resetting.value = true
  error.value = null
  resetMessage.value = null
  resetWarning.value = null
  try {
    const result: OpenAIQuotaResetResult = await resetOpenAIQuota(props.account.id)
    showResetCreditDetails.value = false
    if (result.cache_refreshed && result.quota) {
      data.value = result.quota
      cachedData.value = result.quota
    } else {
      // A credit was consumed but the post-reset count could not be read back.
      // Whatever we still hold is one generation stale, so report the count as
      // unknown instead of letting a second consumption start from stale data.
      data.value = null
    }
    if (result.account) emit('account-updated', result.account)

    if (result.warning_code === 'reset_credit_cache_refresh_failed') {
      resetWarning.value = t('admin.accounts.openaiQuotaReset.resetCacheRefreshFailed')
    } else if (result.warning_code === 'account_state_recovery_failed') {
      resetWarning.value = t('admin.accounts.openaiQuotaReset.resetAccountRecoveryFailed')
    } else if (result.warning_code === 'account_state_refresh_failed') {
      resetWarning.value = t('admin.accounts.openaiQuotaReset.resetAccountRefreshFailed')
    } else {
      resetMessage.value = t('admin.accounts.openaiQuotaReset.resetSuccess', {
        windows: result.windows_reset
      })
    }
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    resetting.value = false
  }
}

watch(
  () => props.account.id,
  () => {
    // Account row may be reused across paginated lists; reset local state.
    cachedData.value = readCachedResetCredits(props.account)
    data.value = cachedData.value
    referralStatus.value = null
    error.value = null
    resetMessage.value = null
    resetWarning.value = null
    inviteDialogOpen.value = false
    inviteMode.value = 'pool'
    inviteError.value = null
    inviteResult.value = null
    selectedTargetAccountID.value = 0
    inviteEmail.value = ''
    targetAccounts.value = []
    loadingAccounts.value = false
    loading.value = false
    resetting.value = false
    inviting.value = false
    showResetConfirm.value = false
    showResetCreditDetails.value = false
  }
)

watch(
  resetCreditExpirations,
  () => {
    if (hiddenResetCreditCount.value <= 0) {
      showResetCreditDetails.value = false
    }
  }
)

const loadTargetAccounts = async () => {
  if (loadingAccounts.value || targetAccounts.value.length > 0) return
  loadingAccounts.value = true
  try {
    const res = await listAccounts(1, 1000, {
      platform: 'openai',
      type: 'oauth',
      sort_by: 'name',
      sort_order: 'asc'
    })
    targetAccounts.value = res.items.filter((item) => item.id !== props.account.id)
  } catch (e) {
    inviteError.value = extractErrorMessage(e)
  } finally {
    loadingAccounts.value = false
  }
}

const openInviteDialog = async () => {
  inviteDialogOpen.value = true
  inviteError.value = null
  inviteResult.value = null
  await loadTargetAccounts()
}

const closeInviteDialog = () => {
  if (inviting.value) return
  inviteDialogOpen.value = false
}

const setInviteMode = (mode: 'pool' | 'email') => {
  inviteMode.value = mode
  inviteError.value = null
  inviteResult.value = null
}

const accountOptionLabel = (account: Account): string => {
  const email = typeof account.credentials?.email === 'string' ? account.credentials.email : ''
  return email ? `${account.name} (${email})` : `${account.name} #${account.id}`
}

const handleSendInvite = async () => {
  if (inviting.value || !canSubmitInvite.value) return
  inviting.value = true
  inviteError.value = null
  inviteResult.value = null
  try {
    const payload =
      inviteMode.value === 'pool'
        ? { target_account_id: selectedTargetAccountID.value, auto_redeem: true }
        : { emails: [inviteEmail.value], auto_redeem: false }
    inviteResult.value = await sendOpenAIReferralInvite(props.account.id, payload)
    referralStatus.value = await queryOpenAIReferralStatus(props.account.id).catch(() => referralStatus.value)
  } catch (e) {
    inviteError.value = extractErrorMessage(e)
  } finally {
    inviting.value = false
  }
}
</script>
