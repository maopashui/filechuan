export type IceConfig = {
  stunServers: string[]
  turn?: { url: string; username: string; credential: string }
}

export type TransferMeta = {
  type: 'meta'
  transferId: string
  files: { id: string; name: string; size: number }[]
}

export type TransferChunk = {
  type: 'chunk'
  transferId: string
  fileId: string
  index: number
  total: number
  data: ArrayBuffer
}

export type ControlMsg =
  | { type: 'offer-transfer'; transferId: string; files: { id: string; name: string; size: number }[] }
  | { type: 'accept-transfer'; transferId: string }
  | { type: 'reject-transfer'; transferId: string }
  | { type: 'cancel-transfer'; transferId: string }
  | { type: 'file-done'; transferId: string; fileId: string }
  | { type: 'transfer-done'; transferId: string }
  | { type: 'error'; message: string }

const CHUNK_SIZE = 16 * 1024
const BUFFER_THRESHOLD = 512 * 1024

/** Works on HTTP LAN IPs where crypto.randomUUID is unavailable (non-secure context). */
export function randomId(): string {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') {
    return c.randomUUID()
  }
  const bytes = new Uint8Array(16)
  if (c && typeof c.getRandomValues === 'function') {
    c.getRandomValues(bytes)
  } else {
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = Math.floor(Math.random() * 256)
    }
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function buildIceServers(cfg: IceConfig): RTCIceServer[] {
  const servers: RTCIceServer[] = cfg.stunServers.map((u) => ({ urls: u }))
  if (cfg.turn?.url) {
    servers.push({
      urls: cfg.turn.url,
      username: cfg.turn.username,
      credential: cfg.turn.credential,
    })
  }
  return servers
}

export type ProgressFn = (p: {
  fileId: string
  fileName: string
  sent: number
  total: number
  overallSent: number
  overallTotal: number
}) => void

const MIME_BY_EXT: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  bmp: 'image/bmp',
  svg: 'image/svg+xml',
  heic: 'image/heic',
  heif: 'image/heif',
  mp4: 'video/mp4',
  mov: 'video/quicktime',
  webm: 'video/webm',
  mp3: 'audio/mpeg',
  wav: 'audio/wav',
  m4a: 'audio/mp4',
  pdf: 'application/pdf',
  zip: 'application/zip',
  gz: 'application/gzip',
  rar: 'application/vnd.rar',
  '7z': 'application/x-7z-compressed',
  doc: 'application/msword',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  ppt: 'application/vnd.ms-powerpoint',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  json: 'application/json',
  html: 'text/html',
  css: 'text/css',
  js: 'text/javascript',
  ts: 'text/plain',
  md: 'text/markdown',
  csv: 'text/csv',
  txt: 'text/plain',
}

/** Avoid empty MIME → browsers (esp. mobile Safari) often save as .txt */
export function mimeFromFilename(name: string): string {
  const base = name.split(/[/\\]/).pop() || name
  const ext = base.includes('.') ? base.split('.').pop()!.toLowerCase() : ''
  if (ext && MIME_BY_EXT[ext]) return MIME_BY_EXT[ext]
  // octet-stream is safer than empty/text for unknown binary
  return 'application/octet-stream'
}

export function sanitizeDownloadName(name: string): string {
  let base = (name.split(/[/\\]/).pop() || 'file').replace(/[<>:"|?*\x00-\x1f]/g, '_')
  base = base.trim() || 'file'
  if (!base.includes('.')) {
    base = `${base}.bin`
  }
  return base.slice(0, 180)
}

export async function sendFiles(
  channel: RTCDataChannel,
  files: File[],
  onProgress: ProgressFn,
  signal?: AbortSignal,
) {
  const transferId = randomId()
  const metas = files.map((f) => ({ id: randomId(), name: f.name, size: f.size }))
  const overallTotal = files.reduce((s, f) => s + f.size, 0)
  let overallSent = 0

  sendJSON(channel, { type: 'offer-transfer', transferId, files: metas } satisfies ControlMsg)

  await waitForControl(channel, (m) => m.type === 'accept-transfer' && m.transferId === transferId, signal)

  for (let i = 0; i < files.length; i++) {
    if (signal?.aborted) throw new Error('cancelled')
    const file = files[i]
    const meta = metas[i]
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE) || 1
    let offset = 0
    let index = 0
    while (offset < file.size || file.size === 0) {
      if (signal?.aborted) {
        sendJSON(channel, { type: 'cancel-transfer', transferId })
        throw new Error('cancelled')
      }
      await waitBuffer(channel)
      const slice = file.slice(offset, offset + CHUNK_SIZE)
      const buf = await slice.arrayBuffer()
      const header = encodeChunkHeader(transferId, meta.id, index, totalChunks, buf.byteLength)
      const packet = new Uint8Array(header.byteLength + buf.byteLength)
      packet.set(new Uint8Array(header), 0)
      packet.set(new Uint8Array(buf), header.byteLength)
      channel.send(packet)
      offset += buf.byteLength
      overallSent += buf.byteLength
      index++
      onProgress({
        fileId: meta.id,
        fileName: meta.name,
        sent: Math.min(offset, file.size),
        total: file.size,
        overallSent,
        overallTotal,
      })
      if (file.size === 0) break
    }
    sendJSON(channel, { type: 'file-done', transferId, fileId: meta.id })
  }
  sendJSON(channel, { type: 'transfer-done', transferId })
}

export type IncomingFile = {
  id: string
  name: string
  size: number
  chunks: Map<number, ArrayBuffer>
  received: number
  totalChunks: number
}

export function createReceiver(handlers: {
  onOffer: (transferId: string, files: { id: string; name: string; size: number }[]) => void
  onProgress: ProgressFn
  onFileReady: (file: File) => void
  onDone: () => void
  onError: (msg: string) => void
  onCancel: () => void
}) {
  const transfers = new Map<string, Map<string, IncomingFile>>()

  return {
    accept(channel: RTCDataChannel, transferId: string) {
      sendJSON(channel, { type: 'accept-transfer', transferId })
    },
    reject(channel: RTCDataChannel, transferId: string) {
      sendJSON(channel, { type: 'reject-transfer', transferId })
    },
    handleMessage(data: ArrayBuffer | string) {
      if (typeof data === 'string') {
        try {
          const msg = JSON.parse(data) as ControlMsg
          if (msg.type === 'offer-transfer') {
            const map = new Map<string, IncomingFile>()
            for (const f of msg.files) {
              map.set(f.id, {
                id: f.id,
                name: f.name,
                size: f.size,
                chunks: new Map(),
                received: 0,
                totalChunks: Math.ceil(f.size / CHUNK_SIZE) || 1,
              })
            }
            transfers.set(msg.transferId, map)
            handlers.onOffer(msg.transferId, msg.files)
          } else if (msg.type === 'cancel-transfer') {
            transfers.delete(msg.transferId)
            handlers.onCancel()
          } else if (msg.type === 'transfer-done') {
            handlers.onDone()
          } else if (msg.type === 'error') {
            handlers.onError(msg.message)
          }
        } catch {
          /* ignore */
        }
        return
      }

      const buf = data
      const parsed = decodeChunk(buf)
      if (!parsed) return
      const fileMap = transfers.get(parsed.transferId)
      if (!fileMap) return
      const file = fileMap.get(parsed.fileId)
      if (!file) return
      file.chunks.set(parsed.index, parsed.payload)
      file.received += parsed.payload.byteLength
      file.totalChunks = parsed.total
      let overallSent = 0
      let overallTotal = 0
      for (const f of fileMap.values()) {
        overallSent += f.received
        overallTotal += f.size
      }
      handlers.onProgress({
        fileId: file.id,
        fileName: file.name,
        sent: file.received,
        total: file.size,
        overallSent,
        overallTotal,
      })

      if (file.chunks.size >= file.totalChunks) {
        const parts: BlobPart[] = []
        for (let i = 0; i < file.totalChunks; i++) {
          const c = file.chunks.get(i)
          if (!c) {
            handlers.onError('missing chunk')
            return
          }
          parts.push(c)
        }
        const type = mimeFromFilename(file.name)
        const blob = new Blob(parts, { type })
        handlers.onFileReady(new File([blob], file.name, { type, lastModified: Date.now() }))
        file.chunks.clear()
      }
    },
  }
}

function sendJSON(channel: RTCDataChannel, msg: unknown) {
  channel.send(JSON.stringify(msg))
}

function waitBuffer(channel: RTCDataChannel) {
  if (channel.bufferedAmount < BUFFER_THRESHOLD) return Promise.resolve()
  return new Promise<void>((resolve) => {
    const check = () => {
      if (channel.bufferedAmount < BUFFER_THRESHOLD / 2) {
        channel.removeEventListener('bufferedamountlow', check)
        resolve()
      }
    }
    channel.bufferedAmountLowThreshold = BUFFER_THRESHOLD / 2
    channel.addEventListener('bufferedamountlow', check)
  })
}

function waitForControl(
  channel: RTCDataChannel,
  pred: (m: ControlMsg) => boolean,
  signal?: AbortSignal,
) {
  return new Promise<void>((resolve, reject) => {
    const onMsg = (ev: MessageEvent) => {
      if (typeof ev.data !== 'string') return
      try {
        const msg = JSON.parse(ev.data) as ControlMsg
        if (msg.type === 'reject-transfer') {
          cleanup()
          reject(new Error('对方拒绝接收'))
          return
        }
        if (pred(msg)) {
          cleanup()
          resolve()
        }
      } catch {
        /* ignore */
      }
    }
    const onAbort = () => {
      cleanup()
      reject(new Error('cancelled'))
    }
    const cleanup = () => {
      channel.removeEventListener('message', onMsg)
      signal?.removeEventListener('abort', onAbort)
    }
    channel.addEventListener('message', onMsg)
    signal?.addEventListener('abort', onAbort)
  })
}

function encodeChunkHeader(
  transferId: string,
  fileId: string,
  index: number,
  total: number,
  payloadLen: number,
): ArrayBuffer {
  const enc = new TextEncoder()
  const tid = enc.encode(transferId)
  const fid = enc.encode(fileId)
  const header = new ArrayBuffer(2 + tid.length + 2 + fid.length + 4 + 4 + 4)
  const view = new DataView(header)
  let o = 0
  view.setUint16(o, tid.length)
  o += 2
  new Uint8Array(header, o, tid.length).set(tid)
  o += tid.length
  view.setUint16(o, fid.length)
  o += 2
  new Uint8Array(header, o, fid.length).set(fid)
  o += fid.length
  view.setUint32(o, index)
  o += 4
  view.setUint32(o, total)
  o += 4
  view.setUint32(o, payloadLen)
  return header
}

function decodeChunk(buf: ArrayBuffer) {
  try {
    const view = new DataView(buf)
    let o = 0
    const tidLen = view.getUint16(o)
    o += 2
    const transferId = new TextDecoder().decode(new Uint8Array(buf, o, tidLen))
    o += tidLen
    const fidLen = view.getUint16(o)
    o += 2
    const fileId = new TextDecoder().decode(new Uint8Array(buf, o, fidLen))
    o += fidLen
    const index = view.getUint32(o)
    o += 4
    const total = view.getUint32(o)
    o += 4
    const payloadLen = view.getUint32(o)
    o += 4
    const payload = buf.slice(o, o + payloadLen)
    return { transferId, fileId, index, total, payload }
  } catch {
    return null
  }
}
