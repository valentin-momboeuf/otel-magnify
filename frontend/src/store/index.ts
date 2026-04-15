import { create } from 'zustand'
import type { Agent, Alert, RemoteConfigStatus, AutoRollbackEvent } from '../types'

interface AppState {
  agents: Agent[]
  alerts: Alert[]
  configStatus: Record<string, RemoteConfigStatus | undefined>
  lastRollback: Record<string, AutoRollbackEvent | undefined>

  setAgents: (agents: Agent[]) => void
  updateAgent: (agent: Agent) => void
  setAlerts: (alerts: Alert[]) => void
  addAlert: (alert: Alert) => void
  resolveAlert: (id: string) => void

  setConfigStatus: (agentId: string, status: RemoteConfigStatus) => void
  setAutoRollback: (ev: AutoRollbackEvent) => void
  clearAutoRollback: (agentId: string) => void
}

export const useStore = create<AppState>((set) => ({
  agents: [],
  alerts: [],
  configStatus: {},
  lastRollback: {},

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
  addAlert: (alert) => set((state) => ({ alerts: [alert, ...state.alerts] })),
  resolveAlert: (id) =>
    set((state) => ({ alerts: state.alerts.filter((a) => a.id !== id) })),

  setConfigStatus: (agentId, status) =>
    set((state) => ({ configStatus: { ...state.configStatus, [agentId]: status } })),
  setAutoRollback: (ev) =>
    set((state) => ({ lastRollback: { ...state.lastRollback, [ev.agent_id]: ev } })),
  clearAutoRollback: (agentId) =>
    set((state) => {
      const next = { ...state.lastRollback }
      delete next[agentId]
      return { lastRollback: next }
    }),
}))
