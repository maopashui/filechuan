import { create } from 'zustand'
import * as api from '../api/client'

type AuthState = {
  username: string | null
  checked: boolean
  setupNeeded: boolean | null
  refresh: () => Promise<void>
  login: (u: string, p: string) => Promise<void>
  logout: () => Promise<void>
  register: (u: string, p: string) => Promise<void>
}

export const useAuth = create<AuthState>((set) => ({
  username: null,
  checked: false,
  setupNeeded: null,
  async refresh() {
    try {
      const setup = await api.getSetupStatus()
      set({ setupNeeded: setup.setup_needed })
      if (!setup.setup_needed) {
        try {
          const u = await api.me()
          set({ username: u.username, checked: true })
          return
        } catch {
          set({ username: null, checked: true })
          return
        }
      }
      set({ username: null, checked: true })
    } catch {
      set({ checked: true })
    }
  },
  async login(u, p) {
    await api.login(u, p)
    const me = await api.me()
    set({ username: me.username, setupNeeded: false })
  },
  async logout() {
    await api.logout()
    set({ username: null })
  },
  async register(u, p) {
    await api.registerAdmin(u, p)
    const me = await api.me()
    set({ username: me.username, setupNeeded: false })
  },
}))
