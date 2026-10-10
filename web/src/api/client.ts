import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
})

function getCsrf(): string {
  const m = document.cookie.match(/(?:^|; )fc_csrf=([^;]*)/)
  return m ? decodeURIComponent(m[1]) : ''
}

api.interceptors.request.use((config) => {
  const method = (config.method || 'get').toLowerCase()
  if (method !== 'get' && method !== 'head') {
    config.headers.set('X-CSRF-Token', getCsrf())
  }
  return config
})

export type PublicConfig = {
  max_file_size: number
  part_size: number
  stun_servers: string[]
  turn: { url: string; username: string; credential: string }
  room_max_age_sec?: number
  expire_day_options: number[]
}

export async function getSetupStatus() {
  const { data } = await api.get<{ setup_needed: boolean }>('/setup/status')
  return data
}

export async function registerAdmin(username: string, password: string) {
  const { data } = await api.post('/setup/register', { username, password })
  return data
}

export async function login(username: string, password: string) {
  const { data } = await api.post('/auth/login', { username, password })
  return data
}

export async function logout() {
  await api.post('/auth/logout')
}

export async function me() {
  const { data } = await api.get<{ username: string; id: number }>('/auth/me')
  return data
}

export async function getPublicConfig() {
  const { data } = await api.get<PublicConfig>('/config/public')
  return data
}

export type FileItem = {
  id: number
  original_name: string
  size: number
  sha256: string
  status: string
  created_at: string
  expires_at: string
  download_count: number
  pickup_code?: string
  share_token?: string
  pickup_url?: string
}

export async function listFiles(page = 1, pageSize = 20) {
  const { data } = await api.get<{ items: FileItem[]; total: number }>('/files', {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function deleteFile(id: number) {
  await api.delete(`/files/${id}`)
}

export async function regeneratePickup(id: number) {
  const { data } = await api.post<{ pickup_code: string; share_token: string; pickup_url: string }>(
    `/files/${id}/pickup-code`,
  )
  return data
}

export async function revokePickup(id: number) {
  await api.delete(`/files/${id}/pickup-code`)
}

export async function createUpload(filename: string, size: number, expiresDays: number) {
  const { data } = await api.post<{
    id: string
    part_size: number
    total_parts: number
  }>('/uploads', { filename, size, expires_days: expiresDays })
  return data
}

export async function putPart(uploadId: string, part: number, blob: Blob) {
  const { data } = await api.put(`/uploads/${uploadId}/parts/${part}`, blob, {
    headers: { 'Content-Type': 'application/octet-stream' },
  })
  return data
}

export async function completeUpload(uploadId: string) {
  const { data } = await api.post<FileItem>(`/uploads/${uploadId}/complete`)
  return data
}

export async function abortUpload(uploadId: string) {
  await api.delete(`/uploads/${uploadId}`)
}

export async function verifyPickup(code: string) {
  const { data } = await api.post<{
    share_token: string
    original_name: string
    size: number
    expires_at: string
    download_url: string
  }>('/pickup/verify', { code })
  return data
}

export async function createRoom() {
  const { data } = await api.post<{ room: string; join_url: string }>('/rooms')
  return data
}

export default api
