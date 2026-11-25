import assert from 'node:assert/strict'
import { test } from 'node:test'
import { adminRoute, canonicalWorkspaceURL, isPagePath, isProtectedPath, libraryItemId, libraryItemPath, navigationRegion, resourcePath, userToolPath, userToolRoute } from '../src/workspaceRoutes.ts'

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

test('unknown management pages do not fall back to a parent panel', () => {
  for (const path of ['/admin/settings/appearances', '/admin/settings/site/extra', '/admin/missing', '/admin/users/extra', '/admin/plugins/example.plugin/extra']) {
    assert.equal(adminRoute(path), null, path)
    assert.equal(canonicalWorkspaceURL(path), path, path)
  }
})

test('page matching accepts complete routes and rejects extra or misspelled segments', () => {
  const validId = '0123456789abcdef0123456789abcdef'
  for (const path of ['/', '/login', '/library', `/library/${validId}`, `/library/${validId}/`, '/user/account/profile', '/user/resources/upload', '/admin/settings/appearance/', '/admin/plugins/wishingskin.oidc', '/admin/settings', '/user/materials']) assert.equal(isPagePath(path), true, path)
  for (const path of ['/missing', '/library/extra', '/library/short', `/library/${validId}/extra`, '/login/extra', '/user/missing', '/user/account/profiles', '/user/resources/uploads', '/admin/settings/appearances', '/admin/plugins/example.plugin/extra', '/admin/plugins/%invalid']) assert.equal(isPagePath(path), false, path)
  assert.equal(libraryItemId(`/library/${validId}`), validId)
  assert.equal(libraryItemId('/library'), '')
  assert.equal(libraryItemPath(validId), `/library/${validId}`)
  assert.equal(libraryItemPath(''), '/library')
  assert.equal(navigationRegion('/library', `/library/${validId}`), '')
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

test('user tool routes match declared segments and protect the workspace', () => {
  assert.equal(userToolPath(), '/user/tools')
  assert.equal(userToolPath('wishingskin.skin-editor', 'editor'), '/user/tools/wishingskin.skin-editor/editor')
  assert.deepEqual(userToolRoute('/user/tools/wishingskin.skin-editor/editor/'), { pluginId: 'wishingskin.skin-editor', frontendId: 'editor' })
  assert.equal(isPagePath('/user/tools'), true)
  assert.equal(isPagePath('/user/tools/wishingskin.skin-editor/editor'), true)
  assert.equal(isProtectedPath('/user/tools'), true)
  assert.equal(isProtectedPath('/user/tools/wishingskin.skin-editor/editor'), true)
  assert.equal(navigationRegion('/user/tools', '/user/tools/wishingskin.skin-editor/editor'), '[data-panel-region="tools"]')

  const longSegment = 'a'.repeat(65)
  for (const bad of ['/user/tools/only-one', '/user/tools/a/b/c', `/user/tools/${longSegment}/ok`, `/user/tools/ok/${longSegment}`, '/user/tools/%bad/id']) {
    assert.equal(userToolRoute(bad), null, bad)
    assert.equal(isPagePath(bad), false, bad)
  }
})
