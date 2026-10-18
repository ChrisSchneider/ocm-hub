import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import type { ConnectorFacet } from '../api/client'
import { SourcesFacet } from './SourcesFacet'

const values: ConnectorFacet[] = [
  {
    key: 'github:enterprise:acme', title: 'Acme Enterprise', type: 'github/v1', avatarUrl: 'https://avatars.example/ent', count: 3,
    groups: [
      { key: 'github:org:acme-corp', title: 'Acme Corporation', count: 2 },
      { key: 'github:org:acme-labs', title: 'Acme Labs', count: 1 },
    ],
  },
  { key: 'github:user:jane', title: 'jane', type: 'github/v1', count: 1, groups: [] },
]

function Harness({ onChange }: { onChange: (s: Set<string>) => void }) {
  const [sel, setSel] = useState<ReadonlySet<string>>(new Set())
  return <SourcesFacet values={values} selected={sel} onChange={(s) => { setSel(s); onChange(s) }} />
}

test('groups are collapsed and expand on demand; plain sources have no expander', async () => {
  const user = userEvent.setup()
  render(<Harness onChange={() => {}} />)
  expect(screen.queryByRole('checkbox', { name: /Acme Labs/ })).toBeNull()
  expect(screen.getAllByRole('button', { name: /Show orgs/ })).toHaveLength(1)
  await user.click(screen.getByRole('button', { name: 'Show orgs of Acme Enterprise' }))
  expect(screen.getByRole('checkbox', { name: /Acme Labs/ })).toBeInTheDocument()
  expect(within(screen.getByRole('group', { name: 'Sources' })).getByText('(2)')).toBeInTheDocument()
})

test('child selection makes the parent indeterminate; parent selection replaces children', async () => {
  const user = userEvent.setup()
  const changes: string[][] = []
  render(<Harness onChange={(s) => changes.push([...s].sort())} />)
  await user.click(screen.getByRole('button', { name: 'Show orgs of Acme Enterprise' }))
  await user.click(screen.getByRole('checkbox', { name: /Acme Labs/ }))
  const parent = screen.getByRole('checkbox', { name: /Acme Enterprise/ }) as HTMLInputElement
  expect(parent.indeterminate).toBe(true)
  expect(changes.at(-1)).toEqual(['github:org:acme-labs'])
  await user.click(parent)
  expect(changes.at(-1)).toEqual(['github:enterprise:acme'])
  expect(parent.indeterminate).toBe(false)
  await user.click(parent)
  expect(changes.at(-1)).toEqual([])
})

const before = (a: Node, b: Node) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0

test('sources are grouped under their connector type; a row reads checkbox, avatar, title, count, expander', () => {
  render(<Harness onChange={() => {}} />)
  const group = screen.getByRole('group', { name: 'GitHub' })
  expect(within(group).getByText('GitHub')).toBeInTheDocument()
  expect(within(group).getByLabelText('GitHub', { selector: 'svg' })).toBeInTheDocument() // the type mark sits in the header only
  expect(within(group).getAllByLabelText('GitHub', { selector: 'svg' })).toHaveLength(1)
  const box = within(group).getByRole('checkbox', { name: /Acme Enterprise/ })
  const title = within(group).getByText('Acme Enterprise')
  const avatar = group.querySelector('img[src="https://avatars.example/ent"]')!
  const count = within(group).getByText('(3)')
  const expander = within(group).getByRole('button', { name: 'Show orgs of Acme Enterprise' })
  expect(before(box, avatar) && before(avatar, title) && before(title, count) && before(count, expander)).toBe(true)
})
