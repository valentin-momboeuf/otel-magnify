import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import type {
  AxiosAdapter,
  AxiosError,
  AxiosRequestConfig,
  AxiosResponse,
  InternalAxiosRequestConfig,
} from 'axios'
import ts from 'typescript'
import api from '../../src/api/client.ts'
import {
  opampTokensAPI,
  type CreateOpAMPTokenResponse,
  type OpAMPTokenMetadata,
  type OpAMPTokenMutationError,
} from '../../src/api/opampTokens.ts'
import { queryClient } from '../../src/api/queryClient.ts'
import { opampTokenKeys } from '../../src/api/queryKeys.ts'

const tokenMetadata: OpAMPTokenMetadata = {
  id: 'token-public-id',
  name: 'production collectors',
  description: 'Collectors managed by platform engineering',
  team: 'platform',
  environment: 'production',
  created_at: '2026-07-23T08:00:00Z',
  created_by: 'admin@example.com',
  expires_at: '2026-08-23T08:00:00Z',
  status: 'active',
}

type StorageWrite = {
  storage: 'localStorage' | 'sessionStorage'
  key: string
  value: string
}

function installStorageMocks() {
  const writes: StorageWrite[] = []
  const storage = (storageName: StorageWrite['storage']): Storage => ({
    getItem: () => null,
    setItem: (key: string, value: string) => {
      writes.push({ storage: storageName, key, value })
    },
    removeItem: () => undefined,
    clear: () => undefined,
    key: () => null,
    length: 0,
  })

  Object.defineProperties(globalThis, {
    localStorage: { configurable: true, value: storage('localStorage') },
    sessionStorage: { configurable: true, value: storage('sessionStorage') },
  })

  return writes
}

function storageWritesContain(writes: StorageWrite[], value: string) {
  return writes.some((write) => write.key.includes(value) || write.value.includes(value))
}

async function withAdapter<T>(adapter: AxiosAdapter, run: () => Promise<T>) {
  const previousAdapter = api.defaults.adapter
  api.defaults.adapter = adapter
  try {
    return await run()
  } finally {
    api.defaults.adapter = previousAdapter
  }
}

function response(config: InternalAxiosRequestConfig, data: unknown, status = 200): AxiosResponse {
  return {
    data,
    status,
    statusText: status === 201 ? 'Created' : 'OK',
    headers: {},
    config,
  }
}

function requestContains(config: AxiosRequestConfig, value: string) {
  const requestSurface = JSON.stringify({
    url: config.url,
    params: config.params,
    headers: config.headers,
    data: config.data,
  })
  return requestSurface.includes(value)
}

test('list uses the versioned endpoint and normalizes a missing tokens property', async () => {
  installStorageMocks()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return response(config, {})
    },
    () => opampTokensAPI.list(),
  )

  assert.deepEqual(result, [])
  assert.equal(seenConfig?.baseURL, '/api')
  assert.equal(seenConfig?.method, 'get')
  assert.equal(seenConfig?.url, '/v1/opamp/tokens')
})

test('create omits only exactly empty optional fields and returns the one-shot value separately', async () => {
  const storageWrites = installStorageMocks()
  const rawToken = ['opamp', 'one-shot', 'credential'].join('_')
  let seenConfig: AxiosRequestConfig | undefined
  const createResponse: CreateOpAMPTokenResponse = {
    token: tokenMetadata,
    value: rawToken,
  }

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return response(config, createResponse, 201)
    },
    () =>
      opampTokensAPI.create({
        name: 'production collectors',
        description: '',
        team: ' ',
        environment: '',
        expires_at: '  ',
      }),
  )

  assert.deepEqual(JSON.parse(String(seenConfig?.data)), {
    name: 'production collectors',
    team: ' ',
    expires_at: '  ',
  })
  assert.equal(result.token.id, tokenMetadata.id)
  assert.equal(result.value === rawToken, true)
  assert.equal('value' in result.token, false)
  assert.equal(requestContains(seenConfig!, rawToken), false)
  assert.equal(storageWritesContain(storageWrites, rawToken), false)
})

test('revoke encodes the public id and sends no body or prior raw value', async () => {
  const storageWrites = installStorageMocks()
  const rawToken = ['opamp', 'one-shot', 'credential'].join('_')
  const seenConfigs: AxiosRequestConfig[] = []
  const id = 'team/platform token?#'

  await withAdapter(
    async (config) => {
      seenConfigs.push(config)
      if (config.url === '/v1/opamp/tokens') {
        return response(config, { token: tokenMetadata, value: rawToken }, 201)
      }
      return response(config, {
        token: { ...tokenMetadata, status: 'revoked' },
        disconnected_connections: 2,
      })
    },
    async () => {
      const created = await opampTokensAPI.create({ name: 'production collectors' })
      assert.equal(created.value === rawToken, true)
      await opampTokensAPI.revoke(id)
    },
  )

  const revokeConfig = seenConfigs[1]
  assert.equal(revokeConfig?.method, 'post')
  assert.equal(revokeConfig?.url, '/v1/opamp/tokens/team%2Fplatform%20token%3F%23/revoke')
  assert.equal(revokeConfig?.data, undefined)
  assert.equal(requestContains(revokeConfig!, rawToken), false)
  assert.equal(storageWritesContain(storageWrites, rawToken), false)
})

test('a rejected commit-unknown response preserves only reconciliation-safe error data', async () => {
  installStorageMocks()
  const errorData: OpAMPTokenMutationError = {
    error: 'token persistence outcome is unknown',
    side_effect_status: 'unknown',
    token_id: tokenMetadata.id,
  }

  await assert.rejects(
    withAdapter(
      async (config) => {
        const error = {
          name: 'AxiosError',
          message: 'Request failed with status code 503',
          isAxiosError: true,
          config,
          response: response(config, errorData, 503),
        } as AxiosError<OpAMPTokenMutationError>
        return Promise.reject(error)
      },
      () => opampTokensAPI.create({ name: 'production collectors' }),
    ),
    (error: AxiosError<OpAMPTokenMutationError>) => {
      const data = error.response?.data
      return (
        data?.error === 'token persistence outcome is unknown' &&
        data.side_effect_status === 'unknown' &&
        data.token_id === tokenMetadata.id &&
        !Object.keys(data).some((key) => key === 'value' || key.includes('hash'))
      )
    },
  )
})

test('query keys are metadata-only and exact', () => {
  const rawToken = ['opamp', 'one-shot', 'credential'].join('_')

  assert.deepEqual(opampTokenKeys.all, ['admin', 'opamp', 'tokens'])
  assert.deepEqual(opampTokenKeys.list(), ['admin', 'opamp', 'tokens', 'list'])
  assert.equal(JSON.stringify(opampTokenKeys).includes(rawToken), false)
})

test('storage spies capture keys and values for both browser storage APIs', () => {
  const storageWrites = installStorageMocks()
  const rawToken = ['opamp', 'storage', 'credential'].join('_')

  localStorage.setItem(rawToken, 'local-safe-value')
  localStorage.setItem('local-safe-key', rawToken)
  sessionStorage.setItem(rawToken, 'session-safe-value')
  sessionStorage.setItem('session-safe-key', rawToken)

  assert.equal(
    storageWrites.some((write) => write.storage === 'localStorage' && write.key === rawToken),
    true,
  )
  assert.equal(
    storageWrites.some((write) => write.storage === 'localStorage' && write.value === rawToken),
    true,
  )
  assert.equal(
    storageWrites.some((write) => write.storage === 'sessionStorage' && write.key === rawToken),
    true,
  )
  assert.equal(
    storageWrites.some((write) => write.storage === 'sessionStorage' && write.value === rawToken),
    true,
  )
})

test('direct create and subsequent calls leave the shared query and mutation caches secret-free', async () => {
  installStorageMocks()
  queryClient.clear()
  const rawToken = ['opamp', 'cache', 'credential'].join('_')
  let returnedSecretMatched = false

  try {
    await withAdapter(
      async (config) => {
        if (config.method === 'get') {
          return response(config, { tokens: [tokenMetadata] })
        }
        if (config.url?.endsWith('/revoke')) {
          return response(config, {
            token: { ...tokenMetadata, status: 'revoked' },
            disconnected_connections: 1,
          })
        }
        return response(config, { token: tokenMetadata, value: rawToken }, 201)
      },
      async () => {
        const created = await opampTokensAPI.create({ name: tokenMetadata.name })
        returnedSecretMatched = created.value === rawToken
        await opampTokensAPI.list()
        await opampTokensAPI.revoke(tokenMetadata.id)
      },
    )

    const cacheSurface = {
      queries: queryClient
        .getQueryCache()
        .getAll()
        .map((query) => ({ queryKey: query.queryKey, state: query.state, meta: query.meta })),
      mutations: queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => ({
          mutationKey: mutation.options.mutationKey,
          meta: mutation.options.meta,
          state: mutation.state,
        })),
    }

    assert.equal(returnedSecretMatched, true)
    assert.equal(JSON.stringify(cacheSurface).includes(rawToken), false)
    assert.equal(cacheSurface.queries.length, 0)
    assert.equal(cacheSurface.mutations.length, 0)
  } finally {
    queryClient.clear()
  }
})

test('the API module boundary excludes storage, query caches, and extra module-level state', () => {
  const source = readFileSync('src/api/opampTokens.ts', 'utf8')
  const sourceFile = ts.createSourceFile(
    'opampTokens.ts',
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  )
  const importSpecifiers = sourceFile.statements
    .filter(ts.isImportDeclaration)
    .map((declaration) => declaration.moduleSpecifier)
    .filter(ts.isStringLiteral)
    .map((specifier) => specifier.text)
  const forbiddenIdentifiers = new Set([
    'localStorage',
    'sessionStorage',
    'queryClient',
    'QueryClient',
    'useMutation',
  ])
  const moduleLifetimeVariables: Array<{ isConst: boolean; name: string }> = []
  let hasForbiddenIdentifier = false

  function visit(node: ts.Node) {
    if (ts.isIdentifier(node) && forbiddenIdentifiers.has(node.text)) {
      hasForbiddenIdentifier = true
    }
    if (ts.isVariableDeclaration(node) && ts.isVariableDeclarationList(node.parent)) {
      moduleLifetimeVariables.push({
        isConst: (node.parent.flags & ts.NodeFlags.Const) !== 0,
        name: ts.isIdentifier(node.name) ? node.name.text : '',
      })
    }
    ts.forEachChild(node, visit)
  }
  visit(sourceFile)

  assert.equal(
    importSpecifiers.every((specifier) => specifier === './client.ts'),
    true,
  )
  assert.equal(hasForbiddenIdentifier, false)
  assert.deepEqual(moduleLifetimeVariables, [{ isConst: true, name: 'opampTokensAPI' }])
})
