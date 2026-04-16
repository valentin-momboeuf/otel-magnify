import { useStore } from '../store'
import { queryClient } from './queryClient'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

function dispatch(data: {
  type: string
  agent_id?: string
  agent?: Parameters<ReturnType<typeof useStore.getState>['updateAgent']>[0]
  alert?: Parameters<ReturnType<typeof useStore.getState>['addAlert']>[0]
  status?: Parameters<ReturnType<typeof useStore.getState>['setConfigStatus']>[1]
  from_hash?: string
  to_hash?: string
  reason?: string
}) {
  const store = useStore.getState()

  switch (data.type) {
    case 'agent_update':
      if (!data.agent) break
      store.updateAgent(data.agent)
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent', data.agent.id] })
      break
    case 'alert_update':
      if (data.alert) store.addAlert(data.alert)
      queryClient.invalidateQueries({ queryKey: ['alerts'] })
      break
    case 'agent_config_status':
      if (!data.agent_id || !data.status) break
      store.setConfigStatus(data.agent_id, data.status)
      queryClient.invalidateQueries({ queryKey: ['agent', data.agent_id] })
      queryClient.invalidateQueries({ queryKey: ['agent-config-history', data.agent_id] })
      break
    case 'auto_rollback_applied':
      if (!data.agent_id || !data.from_hash || !data.to_hash) break
      store.setAutoRollback({
        agent_id: data.agent_id,
        from_hash: data.from_hash,
        to_hash: data.to_hash,
        reason: data.reason ?? '',
      })
      queryClient.invalidateQueries({ queryKey: ['agent', data.agent_id] })
      queryClient.invalidateQueries({ queryKey: ['agent-config-history', data.agent_id] })
      break
  }
}

export function connectWS() {
  if (ws?.readyState === WebSocket.OPEN) return

  const token = localStorage.getItem('token')
  if (!token) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${window.location.host}/ws?token=${token}`)

  ws.onmessage = (event) => {
    dispatch(JSON.parse(event.data))
  }

  ws.onclose = () => {
    reconnectTimer = setTimeout(connectWS, 3000)
  }

  ws.onerror = () => {
    ws?.close()
  }
}

export function disconnectWS() {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  ws?.close()
  ws = null
}

// Test-only hook exposed on window to let Playwright simulate WS events
// without a live backend. No-op in production when nothing calls it.
if (typeof window !== 'undefined') {
  interface TestWindow {
    __testWsInject?: (ev: unknown) => void
  }
  ;(window as unknown as TestWindow).__testWsInject = (ev) => {
    dispatch(ev as Parameters<typeof dispatch>[0])
  }
}
