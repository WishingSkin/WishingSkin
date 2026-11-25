import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { ref } from 'vue'
import {
  clearUnsavedGuards,
  confirmLeave,
  hasUnsavedChanges,
  hasUnsavedForTag,
  shouldConfirmLeave,
  useUnsavedGuard,
} from '../src/unsavedGuard.ts'

afterEach(() => {
  clearUnsavedGuards()
})

test('keepWithin exempts kept-alive sibling navigation and guards external exit', async () => {
  const dirty = ref(false)
  const unregister = useUnsavedGuard(dirty, '/admin/settings/', 'settings-appearance')

  assert.equal(hasUnsavedChanges(), false)
  assert.equal(shouldConfirmLeave('/admin/users'), false)

  dirty.value = true
  assert.equal(hasUnsavedChanges(), true)
  assert.equal(hasUnsavedForTag('settings-appearance'), true)
  assert.equal(shouldConfirmLeave('/admin/settings/site'), false)
  assert.equal(shouldConfirmLeave('/admin/settings/services?tab=mail'), false)
  assert.equal(shouldConfirmLeave('/admin/users'), true)

  let asked = 0
  const keepEditing = await confirmLeave('/admin/users', async () => {
    asked++
    return false
  })
  assert.equal(keepEditing, false)
  assert.equal(asked, 1)

  const stayInternal = await confirmLeave('/admin/settings/operations', async () => {
    asked++
    return false
  })
  assert.equal(stayInternal, true)
  assert.equal(asked, 1)

  unregister()
  assert.equal(hasUnsavedChanges(), false)
  assert.equal(hasUnsavedForTag('settings-appearance'), false)
})

test('unkept v-if panel keepWithin blocks sibling tab switches while allowing same path', async () => {
  const dirty = ref(true)
  useUnsavedGuard(dirty, '/admin/plugins/wishingskin.oauth2')

  assert.equal(shouldConfirmLeave('/admin/plugins/wishingskin.oauth2'), false)
  assert.equal(shouldConfirmLeave('/admin/plugins'), true)
  assert.equal(shouldConfirmLeave('/admin/plugins/wishingskin.oidc'), true)

  const pointsDirty = ref(true)
  clearUnsavedGuards()
  useUnsavedGuard(pointsDirty, '/admin/plugins', 'plugin:wishingskin.points')
  assert.equal(hasUnsavedForTag('plugin:wishingskin.points'), true)
  assert.equal(shouldConfirmLeave('/admin/plugins/wishingskin.invitations'), false)
  assert.equal(shouldConfirmLeave('/admin/users'), true)
})
