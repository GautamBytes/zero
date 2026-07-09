export interface ZeroClientOptions {
  baseUrl: string
  token?: string
  fetch?: typeof fetch
}

export interface ZeroErrorBody {
  error: {
    code: string
    message: string
  }
}

export class ZeroAPIError extends Error {
  status?: number
  code?: string
  body?: unknown
}

export interface PromptRequest {
  content: string
  model?: string
  reasoningEffort?: string
  permissionMode?: 'auto' | 'ask' | 'unsafe' | 'spec-draft' | string
  autonomy?: 'low' | 'medium' | 'high' | string
  images?: Array<{ mediaType: string; data: string }>
}

export interface RunResult {
  runId: string
  sessionId: string
  finalAnswer?: string
  status: string
  exitCode: number
}

export interface SessionCreate {
  sessionId?: string
  title?: string
  cwd?: string
  modelId?: string
  provider?: string
  tag?: string
}

export interface SessionUpdate {
  title: string
}

export interface PermissionDecision {
  action: string
  reason?: string
}

export interface AskAnswer {
  answers: string[]
}

export interface SubscribeOptions {
  sessionId?: string
  signal?: AbortSignal
  headers?: HeadersInit
}

export interface ZeroClient {
  global: {
    health(): Promise<unknown>
  }
  openapi(): Promise<unknown>
  config: {
    get(): Promise<unknown>
  }
  provider: {
    get(): Promise<unknown>
  }
  models: {
    list(): Promise<unknown>
  }
  path: {
    get(): Promise<unknown>
  }
  vcs: {
    get(): Promise<unknown>
  }
  session: {
    list(): Promise<unknown>
    create(body?: SessionCreate): Promise<unknown>
    get(id: string): Promise<unknown>
    update(id: string, body: SessionUpdate): Promise<unknown>
    eventLog(id: string): Promise<unknown>
    children(id: string): Promise<unknown>
    lineage(id: string): Promise<unknown>
    tree(id: string): Promise<unknown>
    fork(id: string, body?: SessionCreate): Promise<unknown>
    abort(id: string): Promise<unknown>
    message(id: string, body: PromptRequest): Promise<RunResult>
    promptAsync(id: string, body: PromptRequest): Promise<void>
    permission(id: string, permissionId: string, body: PermissionDecision): Promise<unknown>
    ask(id: string, askId: string, body: AskAnswer): Promise<unknown>
  }
  file: {
    get(path: string): Promise<unknown>
    content(path: string): Promise<unknown>
    status(): Promise<unknown>
  }
  find: {
    content(pattern: string): Promise<unknown>
    file(query: string): Promise<unknown>
  }
  event: {
    subscribe(options?: SubscribeOptions): AsyncIterable<unknown>
  }
}

export function createZeroClient(options: ZeroClientOptions): ZeroClient
