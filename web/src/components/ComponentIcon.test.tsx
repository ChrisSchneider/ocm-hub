import { fireEvent, render, screen } from '@testing-library/react'
import { ComponentIcon } from './ComponentIcon'

test('a failed image does not stick when the icon changes', () => {
  const { container, rerender } = render(<ComponentIcon icon="https://a.example/x.png" title="Alpha" />)
  fireEvent.error(container.querySelector('img')!)
  expect(container.querySelector('img')).toBeNull()
  expect(screen.getByText('A')).toBeInTheDocument()
  rerender(<ComponentIcon icon="https://b.example/y.png" title="Alpha" />)
  expect(container.querySelector('img')).toHaveAttribute('src', 'https://b.example/y.png')
})
