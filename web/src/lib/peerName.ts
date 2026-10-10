const STORAGE_KEY = 'fc_peer_name'
const LEGACY_DEFAULTS = new Set(['我', 'me', 'Peer', 'peer'])

function detectBrowser(ua: string): string {
  if (/Edg\//i.test(ua)) return 'Edge'
  if (/OPR\/|Opera/i.test(ua)) return 'Opera'
  if (/Firefox\//i.test(ua)) return 'Firefox'
  if (/Chrome\//i.test(ua) && !/Edg\//i.test(ua)) return 'Chrome'
  if (/Safari\//i.test(ua) && !/Chrome\//i.test(ua)) return 'Safari'
  return '浏览器'
}

function detectDevice(ua: string): string {
  if (/iPhone/i.test(ua)) return 'iPhone'
  if (/iPad/i.test(ua)) return 'iPad'
  if (/Android/i.test(ua)) {
    return /Mobile/i.test(ua) ? 'Android 手机' : 'Android 平板'
  }
  if (/Mac OS X|Macintosh/i.test(ua)) return 'Mac'
  if (/Windows/i.test(ua)) return 'Windows'
  if (/Linux/i.test(ua)) return 'Linux'
  return '设备'
}

/** e.g. "Chrome · Mac", "Safari · iPhone" */
export function suggestPeerName(): string {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  return `${detectBrowser(ua)} · ${detectDevice(ua)}`
}

export function loadPeerName(): string {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)?.trim()
    if (saved && !LEGACY_DEFAULTS.has(saved)) {
      return saved.slice(0, 32)
    }
  } catch {
    /* ignore */
  }
  return suggestPeerName()
}

export function savePeerName(name: string) {
  const v = name.trim().slice(0, 32)
  if (!v) return
  try {
    localStorage.setItem(STORAGE_KEY, v)
  } catch {
    /* ignore */
  }
}
