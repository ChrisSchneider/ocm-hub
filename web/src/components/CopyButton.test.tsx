import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CopyButton } from './CopyButton'

function withoutClipboard(execCommand: boolean) {
  Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
  const exec = vi.fn(() => execCommand)
  Object.defineProperty(document, 'execCommand', { value: exec, configurable: true })
  return exec
}

afterEach(() => {
  // Restore the real property chain: delete our own override.
  Reflect.deleteProperty(navigator, 'clipboard')
  Reflect.deleteProperty(document, 'execCommand')
})

test('falls back to execCommand when the Clipboard API is unavailable', async () => {
  const exec = withoutClipboard(true)
  render(<CopyButton text="ocm get cv x" label="cmd" />)
  await userEvent.click(screen.getByRole('button'))
  expect(exec).toHaveBeenCalledWith('copy')
  expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
  expect(document.querySelector('textarea')).toBeNull()
})

test('shows "Copy failed" and keeps the command in the title when nothing works', async () => {
  withoutClipboard(false)
  render(<CopyButton text="ocm get cv x" label="cmd" />)
  await userEvent.click(screen.getByRole('button'))
  const btn = await screen.findByRole('button', { name: 'Copy failed' })
  expect(btn).toHaveAttribute('title', 'ocm get cv x')
})

test('does not update state after unmount', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  try {
    withoutClipboard(true)
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { unmount } = render(<CopyButton text="x" label="cmd" />)
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(screen.getByRole('button'))
    unmount()
    expect(vi.getTimerCount()).toBe(0)
    errors.mockRestore()
  } finally {
    vi.useRealTimers()
  }
})

test('is an icon button: the label is its accessible name, no visible text', () => {
  render(<CopyButton text="x" label="Copy command" />)
  const btn = screen.getByRole('button', { name: 'Copy command' })
  expect(btn).toHaveTextContent('')
  expect(btn.querySelector('svg')).not.toBeNull()
})
