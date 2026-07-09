import { describe, expect, it } from 'vitest'
import { formatReasoningEffort } from '@/utils/format'

describe('formatReasoningEffort', () => {
  it('formats all current Codex reasoning levels', () => {
    expect(formatReasoningEffort('low')).toBe('Low')
    expect(formatReasoningEffort('medium')).toBe('Medium')
    expect(formatReasoningEffort('high')).toBe('High')
    expect(formatReasoningEffort('xhigh')).toBe('XHigh')
    expect(formatReasoningEffort('max')).toBe('Max')
    expect(formatReasoningEffort('ultra')).toBe('Ultra')
  })

  it('keeps disabled levels empty', () => {
    expect(formatReasoningEffort('minimal')).toBe('-')
    expect(formatReasoningEffort(null)).toBe('-')
  })
})
