// @vitest-environment node
import { readFileSync } from 'node:fs'
// @ts-expect-error untyped helper script
import { generate, outURL } from '../../scripts/schema.mjs'

test('schema.d.ts matches api/openapi.yaml (run `pnpm gen`)', async () => {
  expect(readFileSync(outURL, 'utf8')).toBe(await generate())
})
