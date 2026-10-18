import { writeFileSync } from 'node:fs'
import { generate, outURL } from './schema.mjs'

writeFileSync(outURL, await generate())
