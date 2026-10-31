import assert from 'node:assert/strict'
import { test } from 'node:test'
import { adminRoute, canonicalWorkspaceURL, isProtectedPath, navigationRegion, resourcePath } from '../src/workspaceRoutes.ts'

test('legacy resource and management links keep their destination and filters', () => {
  assert.equal(canonicalWorkspaceURL('/user/materials', '?tab=upload'), '/user/resources/upload')
  assert.equal(canonicalWorkspaceURL('/user/materials/', '?tab=cape&q=blue'), '/user/resources?q=blue&kind=cape')
  assert.equal(canonicalWorkspaceURL('/user/materials', '?tab=skin'), '/user/resources?kind=skin')
  assert.equal(canonicalWorkspaceURL('/admin'), '/admin/users')
  assert.equal(canonicalWorkspaceURL('/admin/textures'), '/admin/resources')
  assert.equal(canonicalWorkspaceURL('/admin/settings'), '/admin/settings/site')
  assert.equal(resourcePath('browse'), '/user/resources')
  assert.equal(resourcePath('upload'), '/user/resources/upload')
})

test('management deep links retain selected panels and require a session', () => {
  assert.deepEqual(adminRoute('/admin/settings/appearance'), { tab: 'settings', settingsTab: 'appearance', pluginId: '' })
  assert.equal(adminRoute('/admin/events').tab, 'events')
  assert.equal(adminRoute('/admin/plugins/wishingskin.oidc').pluginId, 'wishingskin.oidc')
  for (const path of ['/user/resources/upload', '/admin', '/admin/resources', '/admin/settings/operations', '/admin/plugins/wishingskin.oauth2']) {
    assert.equal(isProtectedPath(path), true, path)
  }
  for (const path of ['/', '/library', '/administrator', '/users']) assert.equal(isProtectedPath(path), false, path)
})

test('subpage navigation transitions only the changed content', () => {
  for (const [from, to, panel] of [
    ['/user/resources', '/user/resources/upload', 'resources'],
    ['/user/account', '/user/account/profile', 'account'],
    ['/admin/users', '/admin/events', 'admin'],
    ['/admin/settings/site', '/admin/settings/appearance', 'settings'],
    ['/admin/plugins', '/admin/plugins/wishingskin.oidc', 'plugins'],
  ]) assert.equal(navigationRegion(from, to), `[data-panel-region="${panel}"]`)
  assert.equal(navigationRegion('/admin/events', '/admin/events'), '')
  assert.match(navigationRegion('/user/resources', '/user/account'), /workspace-content/)
})
