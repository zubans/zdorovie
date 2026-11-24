const TOKEN_KEY = 'zdorovie_admin_token'

export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export async function apiFetch(path, opts = {}) {
  const headers = { ...(opts.headers || {}) }
  const token = getToken()
  if (token && !headers.Authorization) {
    headers.Authorization = `Bearer ${token}`
  }
  const isFormData = opts.body instanceof FormData
  const isRaw = opts.body instanceof Blob || opts.body instanceof ArrayBuffer || opts.body instanceof Uint8Array
  if (!isFormData && !isRaw && opts.body && typeof opts.body === 'object' && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
    opts = { ...opts, body: JSON.stringify(opts.body) }
  }

  const resp = await fetch(path, {
    credentials: 'include',
    ...opts,
    headers,
  })
  const text = await resp.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : {}
  } catch {
    data = { message: text }
  }
  if (!resp.ok) {
    const err = new Error(data?.message || `Ошибка HTTP ${resp.status}`)
    err.status = resp.status
    err.code = data?.error || 'http_error'
    throw err
  }
  return data
}

export const formatRub = (kopecks) => `${Math.round((kopecks || 0) / 100).toLocaleString('ru-RU')} ₽`
export const formatDate = (iso) => (iso ? new Date(iso).toLocaleDateString('ru-RU') : '—')
export const formatDateTime = (iso) => (iso ? new Date(iso).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '—')
