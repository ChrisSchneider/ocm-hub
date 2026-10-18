import { render, screen, within } from '@testing-library/react'
import { CodeBlock } from './CodeBlock'

test('shows the text in small monospace with an inline icon copy button', () => {
  render(<CodeBlock text="ocm get cv ghcr.io/acme//acme.org/x:1.0.0" label="Copy command" />)
  const pre = screen.getByText('ocm get cv ghcr.io/acme//acme.org/x:1.0.0')
  expect(pre.tagName).toBe('PRE')
  expect(pre).toHaveClass('text-xs')
  expect(within(pre.parentElement!).getByRole('button', { name: 'Copy command' })).toHaveTextContent('')
})
