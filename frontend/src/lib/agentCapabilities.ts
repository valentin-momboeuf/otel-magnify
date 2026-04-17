import type { Agent } from '../types'

/** Collector with AcceptsRemoteConfig capability — eligible for config push. */
export function isSupervised(agent: Agent): boolean {
  return agent.type === 'collector' && agent.accepts_remote_config === true
}

/**
 * Collector without AcceptsRemoteConfig — Edit is hidden, push returns 409.
 * Not defined as !isSupervised: SDK agents are neither supervised nor read-only collectors.
 * Treats missing accepts_remote_config as read-only (safe default for pre-migration payloads).
 */
export function isReadOnlyCollector(agent: Agent): boolean {
  return agent.type === 'collector' && agent.accepts_remote_config !== true
}
