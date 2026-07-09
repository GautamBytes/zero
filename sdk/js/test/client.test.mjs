import assert from 'node:assert/strict'
import test from 'node:test'
import { ZeroAPIError, createZeroClient } from '../index.js'

test('sends bearer auth and JSON body', async () => {
  const calls = []
  const client = createZeroClient({
    baseUrl: 'http://zero.local',
    token: 'secret',
    fetch: async (url, init) => {
      calls.push({ url: String(url), init })
      return Response.json({ ok: true })
    },
  })

  await client.session.message('s1', { content: 'hello' })

  assert.equal(calls[0].url, 'http://zero.local/session/s1/message')
  assert.equal(calls[0].init.method, 'POST')
  assert.equal(calls[0].init.headers.get('Authorization'), 'Bearer secret')
  assert.equal(calls[0].init.headers.get('Content-Type'), 'application/json')
  assert.equal(calls[0].init.body, '{"content":"hello"}')
})

test('maps error shape to ZeroAPIError', async () => {
  const client = createZeroClient({
    baseUrl: 'http://zero.local',
    fetch: async () =>
      Response.json({ error: { code: 'run_active', message: 'session already has an active run' } }, { status: 409 }),
  })

  await assert.rejects(() => client.session.message('s1', { content: 'hello' }), (error) => {
    assert.ok(error instanceof ZeroAPIError)
    assert.equal(error.status, 409)
    assert.equal(error.code, 'run_active')
    assert.equal(error.message, 'session already has an active run')
    return true
  })
})

test('event.subscribe yields parsed SSE events', async () => {
  const encoder = new TextEncoder()
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('event: text\n'))
      controller.enqueue(encoder.encode('data: {"type":"text","delta":"hi"}\n\n'))
      controller.close()
    },
  })
  const client = createZeroClient({
    baseUrl: 'http://zero.local',
    token: 'secret',
    fetch: async (url, init) => {
      assert.equal(String(url), 'http://zero.local/event?sessionId=s1')
      assert.equal(init.headers.get('Authorization'), 'Bearer secret')
      return new Response(stream, { headers: { 'content-type': 'text/event-stream' } })
    },
  })

  const events = []
  for await (const event of client.event.subscribe({ sessionId: 's1' })) {
    events.push(event)
  }

  assert.deepEqual(events, [{ type: 'text', delta: 'hi' }])
})

test('event.subscribe handles SSE chunks split across line boundaries', async () => {
  const encoder = new TextEncoder()
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(': ping\n\n'))
      controller.enqueue(encoder.encode('event: text\n'))
      controller.enqueue(encoder.encode('data: {"type":"text",'))
      controller.enqueue(encoder.encode('"delta":"hi"}\n\n'))
      controller.close()
    },
  })
  const client = createZeroClient({
    baseUrl: 'http://zero.local',
    fetch: async (url) => {
      assert.equal(String(url), 'http://zero.local/event?sessionId=s1')
      return new Response(stream, { headers: { 'content-type': 'text/event-stream' } })
    },
  })

  const events = []
  for await (const event of client.event.subscribe({ sessionId: 's1' })) {
    events.push(event)
  }

  assert.deepEqual(events, [{ type: 'text', delta: 'hi' }])
})

test('promptAsync returns undefined on 204', async () => {
  const client = createZeroClient({
    baseUrl: 'http://zero.local',
    fetch: async () => new Response(null, { status: 204 }),
  })

  assert.equal(await client.session.promptAsync('s1', { content: 'hello' }), undefined)
})
