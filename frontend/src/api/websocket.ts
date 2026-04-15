import { useStore } from '../store'

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

export function connectWS() {
  if (ws?.readyState === WebSocket.OPEN) return

  const token = localStorage.getItem('token')
  if (!token) return

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${window.location.host}/ws?token=${token}`)

  ws.onmessage = (event) => {
    const data = JSON.parse(event.data)
    const store = useStore.getState()

    switch (data.type) {
      case 'agent_update':
        store.updateAgent(data.agent)
        break
      case 'alert_update':
        store.addAlert(data.alert)
        break
      case 'agent_config_status':
        store.setConfigStatus(data.agent_id, data.status)
        break
      case 'auto_rollback_applied':
        store.setAutoRollback({
          agent_id: data.agent_id,
          from_hash: data.from_hash,
          to_hash: data.to_hash,
          reason: data.reason,
        })
        break
    }
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
    const store = useStore.getState()
    const data = ev as {
      type: string
      agent_id?: string
      agent?: Parameters<typeof store.updateAgent>[0]
      alert?: Parameters<typeof store.addAlert>[0]
      status?: Parameters<typeof store.setConfigStatus>[1]
      from_hash?: string
      to_hash?: string
      reason?: string
    }
    switch (data.type) {
      case 'agent_update':
        if (data.agent) store.updateAgent(data.agent)
        break
      case 'alert_update':
        if (data.alert) store.addAlert(data.alert)
        break
      case 'agent_config_status':
        if (data.agent_id && data.status) store.setConfigStatus(data.agent_id, data.status)
        break
      case 'auto_rollback_applied':
        if (data.agent_id && data.from_hash && data.to_hash) {
          store.setAutoRollback({
            agent_id: data.agent_id,
            from_hash: data.from_hash,
            to_hash: data.to_hash,
            reason: data.reason ?? '',
          })
        }
        break
    }
  }
}
