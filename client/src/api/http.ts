// JSON over HTTP with the Quarel error convention: {"error": {code, message}}.

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryAfter = 0,
  ) {
    super(message)
  }
}

export interface RequestOptions {
  token?: string
  body?: unknown
  signal?: AbortSignal
}

export async function request<T>(base: string, method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (opts.token) headers.Authorization = 'Bearer ' + opts.token
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(base + path, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal ?? AbortSignal.timeout(20_000),
    })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'network', 'service unreachable')
  }
  if (res.status === 204 || res.status === 202) return undefined as T
  const text = await res.text()
  let data: unknown = undefined
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    /* not JSON */
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; retry_after?: number } })?.error
    const retry = err?.retry_after ?? Number(res.headers.get('Retry-After') ?? 0)
    throw new ApiError(res.status, err?.code ?? 'http_' + res.status, err?.message ?? res.statusText, retry)
  }
  return data as T
}
