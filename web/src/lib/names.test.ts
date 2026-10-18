import { displayTitle, humanize, splitName } from './names'

test('humanize turns - and _ into spaces and capitalises words', () => {
  expect(humanize('ecrplugin')).toBe('Ecrplugin')
  expect(humanize('ecr-plugin')).toBe('Ecr Plugin')
  expect(humanize('opencode_base-images')).toBe('Opencode Base Images')
  expect(humanize('a--b')).toBe('A B')
})

test('splitName separates the namespace from the last segment', () => {
  expect(splitName('ocm.software/plugins/ecrplugin')).toEqual({ namespace: 'ocm.software/plugins', last: 'ecrplugin' })
  expect(splitName('example.org')).toEqual({ namespace: '', last: 'example.org' })
})

test('displayTitle prefers the title label, else the humanised last segment', () => {
  expect(displayTitle('openCode Base Images', 'github.com/x/opencode-base-images')).toBe('openCode Base Images')
  expect(displayTitle('ocm.software/plugins/ecrplugin', 'ocm.software/plugins/ecrplugin')).toBe('Ecrplugin') // API fallback = name
  expect(displayTitle(undefined, 'ocm.software/plugins/ecr-plugin')).toBe('Ecr Plugin')
})
