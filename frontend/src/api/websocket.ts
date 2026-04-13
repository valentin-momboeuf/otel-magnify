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
