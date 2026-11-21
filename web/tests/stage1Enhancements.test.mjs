import test from 'node:test'
import assert from 'node:assert/strict'
import { authlibInjectorDragUrl, formatBytes, quotaPercent } from '../src/formatBytes.ts'

test('formatBytes formats binary sizes cleanly', () => {
  assert.equal(formatBytes(0), '0 B')
  assert.equal(formatBytes(-10), '0 B')
  assert.equal(formatBytes(512), '512 B')
  assert.equal(formatBytes(1536), '1.5 KiB')
  assert.equal(formatBytes(256 * 1024 * 1024), '256 MiB')
  assert.equal(formatBytes(10 * 1024 * 1024 * 1024), '10 GiB')
})

test('quotaPercent clamps ratio cleanly and handles unlimited quotas', () => {
  assert.equal(quotaPercent(0, 100), 0)
  assert.equal(quotaPercent(50, 0), 0)
  assert.equal(quotaPercent(50, 200), 25)
  assert.equal(quotaPercent(250, 200), 100)
})

test('authlibInjectorDragUrl builds standard authlib-injector URI', () => {
  assert.equal(
    authlibInjectorDragUrl('https://skin.example.com/api/yggdrasil/'),
    'authlib-injector:yggdrasil-server:https%3A%2F%2Fskin.example.com%2Fapi%2Fyggdrasil%2F',
  )
  assert.equal(authlibInjectorDragUrl('   '), '')
})
