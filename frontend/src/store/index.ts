import { create } from 'zustand'
import type { Workload, Alert, RemoteConfigStatus, AutoRollbackEvent } from '../types'

interface AppState {
  workloads: Workload[]
  alerts: Alert[]
  configStatus: Record<string, RemoteConfigStatus | undefined>
  lastRollback: Record<string, AutoRollbackEvent | undefined>
  connectedInstanceCounts: Record<string, number | undefined>
  driftedInstanceCounts: Record<string, number | undefined>

  setWorkloads: (workloads: Workload[]) => void
  updateWorkload: (workload: Workload) => void
  setAlerts: (alerts: Alert[]) => void
  addAlert: (alert: Alert) => void
  resolveAlert: (id: string) => void

  setConfigStatus: (workloadId: string, status: RemoteConfigStatus) => void
  setAutoRollback: (ev: AutoRollbackEvent) => void
  clearAutoRollback: (workloadId: string) => void

  setInstanceCounts: (workloadId: string, connected: number, drifted: number) => void
}

export const useStore = create<AppState>((set) => ({
  workloads: [],
  alerts: [],
  configStatus: {},
  lastRollback: {},
  connectedInstanceCounts: {},
  driftedInstanceCounts: {},

  setWorkloads: (workloads) => set({ workloads }),

  updateWorkload: (workload) =>
    set((state) => {
      const idx = state.workloads.findIndex((w) => w.id === workload.id)
      if (idx >= 0) {
        const updated = [...state.workloads]
        updated[idx] = { ...updated[idx], ...workload }
        return { workloads: updated }
      }
      return { workloads: [...state.workloads, workload] }
    }),

  setAlerts: (alerts) => set({ alerts }),
  addAlert: (alert) => set((state) => ({ alerts: [alert, ...state.alerts] })),
  resolveAlert: (id) =>
    set((state) => ({ alerts: state.alerts.filter((a) => a.id !== id) })),

  setConfigStatus: (workloadId, status) =>
    set((state) => ({ configStatus: { ...state.configStatus, [workloadId]: status } })),
  setAutoRollback: (ev) =>
    set((state) => ({ lastRollback: { ...state.lastRollback, [ev.workload_id]: ev } })),
  clearAutoRollback: (workloadId) =>
    set((state) => {
      const next = { ...state.lastRollback }
      delete next[workloadId]
      return { lastRollback: next }
    }),

  setInstanceCounts: (workloadId, connected, drifted) =>
    set((state) => ({
      connectedInstanceCounts: { ...state.connectedInstanceCounts, [workloadId]: connected },
      driftedInstanceCounts: { ...state.driftedInstanceCounts, [workloadId]: drifted },
    })),
}))
