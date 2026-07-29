import api from './client.ts'

export type OpAMPTokenStatus = 'active' | 'expired' | 'revoked'

export interface OpAMPTokenMetadata {
  id: string
  name: string
  description: string
  team: string
  environment: string
  created_at: string
  created_by: string
  expires_at?: string
  last_used_at?: string
  revoked_at?: string
  revoked_by?: string
  status: OpAMPTokenStatus
}

export interface CreateOpAMPTokenRequest {
  name: string
  description?: string
  team?: string
  environment?: string
  expires_at?: string
}

export interface CreateOpAMPTokenResponse {
  token: OpAMPTokenMetadata
  value: string
}

export interface RevokeOpAMPTokenResponse {
  token: OpAMPTokenMetadata
  disconnected_connections: number
}

export interface OpAMPTokenMutationError {
  error: string
  side_effect_status?: 'none' | 'applied' | 'unknown'
  token_id?: string
}

interface OpAMPTokenListResponse {
  tokens?: OpAMPTokenMetadata[]
}

export const opampTokensAPI = {
  list: () =>
    api
      .get<OpAMPTokenListResponse>('/v1/opamp/tokens')
      .then((response) => response.data.tokens ?? []),
  create: (request: CreateOpAMPTokenRequest) =>
    api
      .post<CreateOpAMPTokenResponse>('/v1/opamp/tokens', {
        name: request.name,
        ...(request.description === '' ? {} : { description: request.description }),
        ...(request.team === '' ? {} : { team: request.team }),
        ...(request.environment === '' ? {} : { environment: request.environment }),
        ...(request.expires_at === '' ? {} : { expires_at: request.expires_at }),
      })
      .then((response) => response.data),
  revoke: (id: string) =>
    api
      .post<RevokeOpAMPTokenResponse>(`/v1/opamp/tokens/${encodeURIComponent(id)}/revoke`)
      .then((response) => response.data),
}
