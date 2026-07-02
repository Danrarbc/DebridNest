const test = require('node:test')
const assert = require('node:assert/strict')

const presets = require('../lib/presets')

const baseManifest = {
  id: 'com.debridnest.streams',
  version: '1.0.0',
  name: 'DebridNest Streams',
  description: 'Base manifest.',
  behaviorHints: {
    configurable: true,
    configurationRequired: true,
    p2p: false,
  },
  config: [{ key: 'apiToken', type: 'password' }],
}

test('default configured manifests use a distinct configured addon id', () => {
  const manifest = presets.buildConfiguredManifest(baseManifest, {})

  assert.equal(manifest.id, 'com.debridnest.streams')
  assert.equal(manifest.name, 'DebridNest')
  assert.equal(manifest.behaviorHints.p2p, false)
  assert.equal(manifest.behaviorHints.configurable, false)
  assert.equal(manifest.behaviorHints.configurationRequired, false)
  assert.equal(manifest.config, undefined)
})

test('preset config fields produce side-by-side manifest ids and defaults', () => {
  const manifest = presets.buildConfiguredManifest(baseManifest, { preset: '1080p SDR' })
  const defaults = presets.configuredDefaults({ manifestPreset: '1080p-sdr' })

  assert.equal(manifest.id, 'com.debridnest.streams.1080p-sdr')
  assert.equal(manifest.name, 'DebridNest: 1080p SDR')
  assert.match(manifest.description, /SDR-friendly/)
  assert.deepEqual(defaults, {
    maxResolution: '1080',
    maxFileSizeGb: '20',
    preferSdr: 'true',
    qualityFilter: '1080p,720p',
    sourceFilter: 'BluRay,WEB-DL,WEBRip,HDTV',
  })
})

test('custom manifest labels can be supplied in config URLs', () => {
  const manifest = presets.buildConfiguredManifest(baseManifest, {
    preset: 'Default',
    manifestLabel: 'Living Room 4K',
  })

  assert.equal(manifest.id, 'com.debridnest.streams.living-room-4k')
  assert.equal(manifest.name, 'DebridNest: Living Room 4K')
})
