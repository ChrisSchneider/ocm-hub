// Shared by `pnpm gen` and the freshness test.
import openapiTS, { astToString } from 'openapi-typescript'

export const specURL = new URL('../../api/openapi.yaml', import.meta.url)
export const outURL = new URL('../src/api/schema.d.ts', import.meta.url)

export async function generate() {
  const ast = await openapiTS(specURL)
  return '// Generated from api/openapi.yaml by `pnpm gen`. Do not edit.\n\n' + astToString(ast)
}
