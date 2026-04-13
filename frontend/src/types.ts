export interface Agent {
  id: string
  display_name: string
  type: 'collector' | 'sdk'
  version: string
  status: 'connected' | 'disconnected' | 'degraded'
  last_seen_at: string
  labels: Record<string, string>
  active_config_id?: string
}

export interface Config {
  id: string
  name: string
  content: string
  created_at: string
  created_by: string
}

export interface Alert {
  id: string
  agent_id: string
  rule: 'agent_down' | 'config_drift' | 'version_outdated'
  severity: 'warning' | 'critical'
  message: string
  fired_at: string
  resolved_at?: string
}

export interface AgentConfig {
  agent_id: string
  config_id: string
  applied_at: string
  status: 'pending' | 'applied' | 'failed'
}
