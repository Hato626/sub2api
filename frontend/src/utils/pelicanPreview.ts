import DOMPurify from 'dompurify'

// Model output is untrusted. Keep its HTML/CSS/SVG inside an opaque, scriptless
// iframe; never inject it into the admin document with v-html.
export function extractPelicanMarkup(output: string): string {
  const text = output.replace(/<think\b[^>]*>[\s\S]*?<\/think>/gi, '').trim()
  const blocks = [...text.matchAll(/```(?:html|svg|xml)?[^\S\r\n]*\r?\n([\s\S]*?)```/gi)]
    .map((match) => match[1].trim())
  const candidates = blocks.length ? blocks : [text]
  for (const candidate of candidates) {
    const document = candidate.match(/(?:<!doctype\s+html[^>]*>\s*)?<html\b[\s\S]*?<\/html\s*>/i)
    if (document) return document[0]
  }
  for (const candidate of candidates) {
    if (/<(?:svg|div|main|section|body)\b/i.test(candidate)) {
      // A fenced fragment can include a sibling <style>. For an unfenced
      // response, discard explanatory prose around the markup.
      const start = candidate.search(/<(?:svg|div|main|section|body|style)\b/i)
      return candidate.slice(start, candidate.lastIndexOf('>') + 1)
    }
  }
  return ''
}

export function buildPelicanPreview(output: string): string {
  const markup = extractPelicanMarkup(output)
  if (!markup) return ''
  const document = DOMPurify.sanitize(markup, {
    WHOLE_DOCUMENT: true,
    RETURN_DOM: true,
    USE_PROFILES: { html: true, svg: true, svgFilters: true },
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'base', 'link', 'meta'],
    FORBID_ATTR: ['srcdoc', 'action', 'formaction', 'target', 'ping', 'download']
  }) as HTMLElement
  // Preserve SVG fragment references (gradients, filters, <use>), but remove
  // navigation and remote resources. CSP also blocks URLs inside CSS/SVG.
  for (const element of document.querySelectorAll('*')) {
    for (const attribute of ['href', 'xlink:href', 'src', 'srcset', 'poster']) {
      const value = element.getAttribute(attribute)
      if (value !== null && !value.startsWith('#') && !(attribute === 'src' && /^data:image\//i.test(value))) {
        element.removeAttribute(attribute)
      }
    }
  }
  const head = document.querySelector('head')!
  const policy = window.document.createElement('meta')
  policy.httpEquiv = 'Content-Security-Policy'
  policy.content = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'"
  head.prepend(policy)
  const viewport = window.document.createElement('meta')
  viewport.name = 'viewport'
  viewport.content = 'width=device-width, initial-scale=1'
  head.append(viewport)
  return '<!doctype html>\n' + document.outerHTML
}
