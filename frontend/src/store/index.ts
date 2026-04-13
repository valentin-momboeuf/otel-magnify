import { create } from 'zustand'
import type { Agent, Alert } from '../types'

interface AppState {
  agents: Agent[]
  alerts: Alert[]
  setAgents: (agents: Agent[]) => void
  updateAgent: (agent: Agent) => void
  setAlerts: (alerts: Alert[]) => void
  addAlert: (alert: Alert) => void
  resolveAlert: (id: string) => void
}

export const useStore = create<AppState>((set) => ({
  agents: [],
  alerts: [],

  setAgents: (agents) => set({ agents }),

  updateAgent: (agent) =>
    set((state) => {
      const idx = state.agents.findIndex((a) => a.id === agent.id)
      if (idx >= 0) {
        const updated = [...state.agents]
        updated[idx] = { ...updated[idx], ...agent }
        return { agents: updated }
      }
      return { agents: [...state.agents, agent] }
    }),

  setAlerts: (alerts) => set({ alerts }),

  addAlert: (alert) =>
    set((state) => ({ alerts: [alert, ...state.alerts] })),

  resolveAlert: (id) =>
    set((state) => ({
      alerts: state.alerts.filter((a) => a.id !== id),
    })),
}))
