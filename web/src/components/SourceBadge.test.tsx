import { fireEvent, render, screen } from '@testing-library/react'
import { SourceAvatar, TypeMark } from './SourceBadge'

const acme = { title: 'Acme', type: 'github/v1', avatarUrl: 'https://avatars.example/acme' }

test('TypeMark shows the GitHub mark for github/v1 and a generic mark otherwise', () => {
  render(<><TypeMark type="github/v1" /><TypeMark type="artifactory/v1" /></>)
  expect(screen.getByLabelText('GitHub')).toBeInTheDocument()
  expect(screen.getByLabelText('artifactory/v1')).toBeInTheDocument()
})

test('SourceAvatar is a decorative image with the title as tooltip', () => {
  const { container } = render(<SourceAvatar source={acme} />)
  const img = container.querySelector('img')!
  expect(img).toHaveAttribute('src', acme.avatarUrl)
  expect(img).toHaveAttribute('alt', '')
  expect(img).toHaveAttribute('title', 'Acme')
})

test('SourceAvatar hides a broken or missing avatar', () => {
  const { container, rerender } = render(<SourceAvatar source={acme} />)
  fireEvent.error(container.querySelector('img')!)
  expect(container.querySelector('img')).toBeNull()
  rerender(<SourceAvatar source={{ ...acme, avatarUrl: undefined }} />)
  expect(container.querySelector('img')).toBeNull()
})
