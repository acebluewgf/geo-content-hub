export function getCookie(name) {
  const m = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'))
  return m ? decodeURIComponent(m[1]) : ''
}

export class ApiError extends Error {
  constructor(message, code, details) {
    super(message)
    this.code = code
    this.details = details
  }
}

export async function api(path, opts = {}) {
  const { method = 'GET', body, form, silent401 = false, idempotencyKey, signal } = opts
  const headers = {}
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && method !== 'HEAD') {
    const token = getCookie('ch_csrf')
    if (token) headers['X-CSRF-Token'] = token
  }
  const res = await fetch('/admin/api' + path, {
    method,
    signal,
    headers,
    credentials: 'same-origin',
    body: form ? form : body !== undefined ? JSON.stringify(body) : undefined,
  })
  let data = {}
  try {
    data = await res.json()
  } catch (e) {
    data = {}
  }
  if (res.status === 401 && !silent401) {
    window.dispatchEvent(new Event('auth:expired'))
  }
  if (!res.ok) {
    throw new ApiError(
      data?.error?.message || `请求失败（${res.status}）`,
      data?.error?.code,
      data?.error?.details
    )
  }
  return data
}

export async function uploadImage(file) {
  const fd = new FormData()
  fd.append('file', file)
  return api('/media', { method: 'POST', form: fd })
}
