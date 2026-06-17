import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpenAIQuotaResetCell from '../OpenAIQuotaResetCell.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params && typeof params.emails === 'string') return `${key}:${params.emails}`
        return key
      }
    })
  }
})

vi.mock('@/api/admin/accounts', () => ({
  list: vi.fn().mockResolvedValue({ items: [] }),
  queryOpenAIQuota: vi.fn(),
  queryOpenAIReferralStatus: vi.fn(),
  resetOpenAIQuota: vi.fn(),
  sendOpenAIReferralInvite: vi.fn()
}))

function makeOpenAIAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 1,
    name: 'sender@example.com',
    platform: 'openai',
    type: 'oauth',
    credentials: { email: 'sender@example.com' },
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-06-18T00:00:00Z',
    updated_at: '2026-06-18T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('OpenAIQuotaResetCell', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('keeps the invite dialog open when switching to email mode', async () => {
    const wrapper = mount(OpenAIQuotaResetCell, {
      props: {
        account: makeOpenAIAccount()
      },
      attachTo: document.body
    })

    await wrapper.findAll('button').find((button) => button.text().includes('invite'))?.trigger('click')
    await flushPromises()

    const buttons = Array.from(document.body.querySelectorAll('button'))
    const emailModeButton = buttons.find((button) =>
      button.textContent?.includes('admin.accounts.openaiQuotaReset.inviteModeEmail')
    )
    expect(emailModeButton).toBeTruthy()

    emailModeButton?.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await flushPromises()

    expect(document.body.textContent).toContain('admin.accounts.openaiQuotaReset.inviteTitle')
    const emailInput = document.body.querySelector<HTMLInputElement>('input[type="email"]')
    expect(emailInput?.placeholder).toBe('admin.accounts.openaiQuotaReset.emailPlaceholder')
  })
})
