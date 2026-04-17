const DEFAULT_DOCS_BASE_URL = 'https://github.com/valentin-momboeuf/otel-magnify/blob/main/docs'

export const DOCS_BASE_URL: string =
  import.meta.env.VITE_DOCS_BASE_URL ?? DEFAULT_DOCS_BASE_URL
