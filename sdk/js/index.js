export class ZeroAPIError extends Error {
  constructor(message, options = {}) {
    super(message)
    this.name = 'ZeroAPIError'
    this.status = options.status
    this.code = options.code
    this.body = options.body
  }
}

export function createZeroClient(options = {}) {
  const baseUrl = normalizeBaseUrl(options.baseUrl)
  const token = options.token
  const fetchImpl = options.fetch ?? globalThis.fetch
  if (typeof fetchImpl !== 'function') {
    throw new TypeError('createZeroClient requires a fetch implementation')
  }

  const raw = (method, path, init = {}) => requestRaw(fetchImpl, baseUrl, token, method, path, init)
  const json = (method, path, init = {}) => requestJSON(raw, method, path, init)

  return {
    global: {
      health: () => json('GET', '/global/health'),
    },
    openapi: () => json('GET', '/openapi.json'),
    config: {
      get: () => json('GET', '/config'),
    },
    provider: {
      get: () => json('GET', '/provider'),
    },
    models: {
      list: () => json('GET', '/models'),
    },
    path: {
      get: () => json('GET', '/path'),
    },
    vcs: {
      get: () => json('GET', '/vcs'),
    },
    session: {
      list: () => json('GET', '/session'),
      create: (body = {}) => json('POST', '/session', { body }),
      get: (id) => json('GET', `/session/${encodeURIComponent(id)}`),
      update: (id, body = {}) => json('PATCH', `/session/${encodeURIComponent(id)}`, { body }),
      eventLog: (id) => json('GET', `/session/${encodeURIComponent(id)}/event-log`),
      children: (id) => json('GET', `/session/${encodeURIComponent(id)}/children`),
      lineage: (id) => json('GET', `/session/${encodeURIComponent(id)}/lineage`),
      tree: (id) => json('GET', `/session/${encodeURIComponent(id)}/tree`),
      fork: (id, body = {}) => json('POST', `/session/${encodeURIComponent(id)}/fork`, { body }),
      abort: (id) => json('POST', `/session/${encodeURIComponent(id)}/abort`, { body: {} }),
      message: (id, body) => json('POST', `/session/${encodeURIComponent(id)}/message`, { body }),
      promptAsync: (id, body) => json('POST', `/session/${encodeURIComponent(id)}/prompt_async`, { body }),
      permission: (id, permissionId, body) =>
        json('POST', `/session/${encodeURIComponent(id)}/permissions/${encodeURIComponent(permissionId)}`, { body }),
      ask: (id, askId, body) =>
        json('POST', `/session/${encodeURIComponent(id)}/ask/${encodeURIComponent(askId)}`, { body }),
    },
    file: {
      get: (path) => json('GET', '/file', { query: { path } }),
      content: (path) => json('GET', '/file/content', { query: { path } }),
      status: () => json('GET', '/file/status'),
    },
    find: {
      content: (pattern) => json('GET', '/find', { query: { pattern } }),
      file: (query) => json('GET', '/find/file', { query: { query } }),
    },
    event: {
      subscribe: (init = {}) => subscribe(raw, init),
    },
  }
}

function normalizeBaseUrl(value) {
  const baseUrl = String(value ?? '').trim()
  if (baseUrl === '') {
    throw new TypeError('createZeroClient requires baseUrl')
  }
  return baseUrl.replace(/\/+$/, '')
}

async function requestRaw(fetchImpl, baseUrl, token, method, path, init = {}) {
  const url = new URL(baseUrl + path)
  for (const [key, value] of Object.entries(init.query ?? {})) {
    if (value !== undefined && value !== null) {
      url.searchParams.set(key, String(value))
    }
  }
  const headers = new Headers(init.headers ?? {})
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  let body
  if (init.body !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(init.body)
  }
  return fetchImpl(url, {
    method,
    headers,
    body,
    signal: init.signal,
  })
}

async function requestJSON(raw, method, path, init) {
  const response = await raw(method, path, init)
  if (!response.ok) {
    throw await toAPIError(response)
  }
  if (response.status === 204) {
    return undefined
  }
  const contentType = response.headers.get('content-type') ?? ''
  if (!contentType.includes('application/json')) {
    return response.text()
  }
  return response.json()
}

async function toAPIError(response) {
  let body
  try {
    body = await response.json()
  } catch {
    body = undefined
  }
  const detail = body?.error
  return new ZeroAPIError(detail?.message ?? `ZERO API request failed with HTTP ${response.status}`, {
    status: response.status,
    code: detail?.code,
    body,
  })
}

async function* subscribe(raw, init = {}) {
  const response = await raw('GET', '/event', {
    query: init.sessionId ? { sessionId: init.sessionId } : undefined,
    signal: init.signal,
    headers: init.headers,
  })
  if (!response.ok) {
    throw await toAPIError(response)
  }
  if (!response.body) {
    throw new ZeroAPIError('SSE response has no body', { status: response.status, code: 'stream_unavailable' })
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let data = []
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) {
        break
      }
      buffer += decoder.decode(value, { stream: true })
      let newline
      while ((newline = buffer.indexOf('\n')) !== -1) {
        const line = buffer.slice(0, newline).replace(/\r$/, '')
        buffer = buffer.slice(newline + 1)
        if (line === '') {
          if (data.length > 0) {
            yield JSON.parse(data.join('\n'))
            data = []
          }
          continue
        }
        if (line.startsWith('data:')) {
          data.push(line.slice(5).trimStart())
        }
      }
    }
    if (data.length > 0) {
      yield JSON.parse(data.join('\n'))
    }
  } finally {
    try {
      await reader.cancel()
    } catch {
      // Stream may already be closed.
    }
    reader.releaseLock()
  }
}
