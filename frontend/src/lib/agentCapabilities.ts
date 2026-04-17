import type { Agent } from '../types'

export function isSupervised(agent: Agent): boolean {
  return agent.type === 'collector' && agent.accepts_remote_config === true
}

export function isReadOnlyCollector(agent: Agent): boolean {
  return agent.type === 'collector' && agent.accepts_remote_config !== true
}
