import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PelicanPreview from '../PelicanPreview.vue'
import AccountTestModal from '../AccountTestModal.vue'
import BatchAccountTestModal from '../BatchAccountTestModal.vue'
import { buildPelicanPreview, extractPelicanMarkup } from '@/utils/pelicanPreview'

const { copyToClipboard, getAvailableModels, getById } = vi.hoisted(() => ({
  copyToClipboard: vi.fn(), getAvailableModels: vi.fn(), getById: vi.fn()
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getAvailableModels, getById } } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

const stubs = {
  BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
  Select: { template: '<div />' }, TextArea: true, Icon: true
}
const svg = '<svg viewBox="0 0 640 420"><defs><linearGradient id="sky"><stop stop-color="cyan"/></linearGradient></defs><rect width="640" height="420" fill="url(#sky)"/><circle id="wheel" cx="200" cy="300" r="60"/></svg>'
const html = '<!doctype html><html><head><style>body { margin: 0; background: white; } svg { width:100%; }</style></head><body>' + svg + '</body></html>'
const fenced = 'Here is the result:\n```html\n' + html + '\n```\nExplanation.'

function stream(text: string) {
  const encoder = new TextEncoder()
  const events = [
    { type: 'test_start', model: 'gpt-5.4' },
    { type: 'content', text: text.slice(0, 123) },
    { type: 'content', text: text.slice(123) },
    { type: 'test_complete', success: true }
  ]
  let index = 0
  return {
    ok: true,
    body: { getReader: () => ({ read: async () => index < events.length
      ? { done: false, value: encoder.encode('data: ' + JSON.stringify(events[index++]) + '\n\n') }
      : { done: true } }) }
  }
}

describe('Pelican rendering', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([{ id: 'gpt-5.4', display_name: 'GPT-5.4' }])
    getById.mockImplementation(async (id: number) => ({ id, name: `Account ${id}`, platform: 'openai' }))
    copyToClipboard.mockReset()
  })
  afterEach(() => vi.unstubAllGlobals())

  it('extracts fenced HTML, preserves CSS and SVG gradients, and omits surrounding explanation', () => {
    expect(extractPelicanMarkup(fenced)).toBe(html)
    const doc = new DOMParser().parseFromString(buildPelicanPreview(fenced), 'text/html')
    expect(doc.querySelector('style')?.textContent).toContain('margin: 0')
    expect(doc.querySelector('svg')?.getAttribute('viewBox')).toBe('0 0 640 420')
    expect(doc.querySelector('linearGradient')?.id).toBe('sky')
    expect(doc.querySelector('rect')?.getAttribute('fill')).toBe('url(#sky)')
    expect(doc.body.textContent).not.toContain('Explanation')
  })

  it('accepts a bare SVG and HTML fragments with sibling CSS', () => {
    expect(buildPelicanPreview('Result: ' + svg)).toContain('id="wheel"')
    expect(buildPelicanPreview('```html\n<style>svg {color:red}</style>' + svg + '\n```')).toContain('color:red')
    expect(extractPelicanMarkup('<think>Draft <svg></svg></think>' + svg)).toBe(svg)
  })

  it('removes scripts, event handlers, navigation and embedded documents while keeping the drawing', () => {
    const source = '<html><head><meta http-equiv="refresh" content="0;url=https://example.com"><base href="https://example.com"></head><body>' + svg +
      '<script>parent.localStorage.clear()</script><iframe src="/admin"></iframe>' +
      '<a href="https://example.com" target="_top">link</a><img src="/api/v1/admin/users" onerror="alert(1)">' +
      '<form action="/api/v1/admin/users"><button formaction="/delete">submit</button></form></body></html>'
    const doc = new DOMParser().parseFromString(buildPelicanPreview(source), 'text/html')
    expect(doc.querySelector('svg')).not.toBeNull()
    expect(doc.querySelector('script,iframe,base,[onerror],[action],[formaction],a[href],img[src],[target]')).toBeNull()
    expect(doc.querySelector('meta[http-equiv="refresh"]')).toBeNull()
    expect(doc.querySelector('head')?.firstElementChild?.getAttribute('content')).toContain("default-src 'none'")
    expect(doc.querySelector('head')?.firstElementChild?.getAttribute('content')).toContain("script-src 'none'")
  })

  it('renders in a sandbox, supports expansion and copies the full original response', async () => {
    const wrapper = mount(PelicanPreview, { props: { source: fenced } })
    const frame = wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes('srcdoc')).toContain('id="wheel"')
    await wrapper.findAll('button')[0].trigger('click')
    expect(frame.classes()).toContain('h-[640px]')
    await wrapper.findAll('button')[1].trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(fenced)
    expect(wrapper.get('pre').text()).toBe(fenced)
    wrapper.unmount()
  })

  it('shows a helpful fallback for a response without HTML', () => {
    const wrapper = mount(PelicanPreview, { props: { source: 'No drawing available.' } })
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.pelicanPreview.noMarkup')
    expect(wrapper.get('pre').text()).toBe('No drawing available.')
    wrapper.unmount()
  })

  it('renders the completed single-account result and clears the preview on mode changes', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => stream(fenced)))
    const wrapper = mount(AccountTestModal, {
      props: { show: false, account: { id: 42, name: 'Test', platform: 'openai', type: 'oauth' } as any },
      global: { stubs }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    ;(wrapper.vm as any).diagnosticMode = 'pelican'
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('id="wheel"')
    ;(wrapper.vm as any).diagnosticMode = 'standard'
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps more than 5000 characters for each parallel batch preview, including its final SVG', async () => {
    const longOutput = '<html><body>' + '<!-- padding -->'.repeat(500) + svg + '</body></html>'
    const fetchMock = vi.fn().mockImplementation(async () => stream(longOutput))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(BatchAccountTestModal, {
      props: { show: true, accountIds: [41, 42] }, global: { stubs }
    })
    await flushPromises()
    await wrapper.findAll('select')[0].setValue('pelican')
    await wrapper.findAll('select')[1].setValue('gpt-5.4')
    await wrapper.findAll('button').find(button => button.text().includes('bulkTest.start'))!.trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    for (const [, request] of fetchMock.mock.calls) expect(JSON.parse(request.body).model_id).toBe('gpt-5.4')
    expect(wrapper.findAll('iframe')).toHaveLength(2)
    for (const frame of wrapper.findAll('iframe')) expect(frame.attributes('srcdoc')).toContain('id="wheel"')
    for (const pre of wrapper.findAll('pre')) expect(pre.text()).toBe(longOutput)
    // Selecting a mode for the next run must not reinterpret the previous output.
    await wrapper.findAll('select')[0].setValue('standard')
    expect(wrapper.findAll('iframe')).toHaveLength(2)
    wrapper.unmount()
  })
})
