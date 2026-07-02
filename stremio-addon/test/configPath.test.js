const test = require('node:test')
const assert = require('node:assert/strict')

const configPath = require('../lib/configPath')

const manifest = {
  config: [
    { key: 'apiUrl', default: 'http://debridnest:8080/rest/1.0' },
    { key: 'jackettUrl', default: 'http://jackett:9117' },
    { key: 'maxResults', default: '20' },
    { key: 'preferSdr', default: 'false' },
  ],
}

test('buildInstallConfig omits URL fields and manifest defaults', () => {
  const config = configPath.buildInstallConfig({
    apiUrl: 'http://debridnest:8080/rest/1.0',
    apiToken: 'abc123',
    jackettUrl: 'http://jackett:9117',
    jackettApiKey: 'key',
    maxResults: '20',
    preferSdr: 'false',
    preset: 'Default',
  }, configPath.manifestDefaultValues(manifest))

  assert.deepEqual(config, {
    apiToken: 'abc123',
    jackettApiKey: 'key',
    preset: 'Default',
  })
  assert.doesNotMatch(encodeURIComponent(JSON.stringify(config)), /%2F/i)
})

test('repairConfiguredPath re-encodes minimal token-only config URLs', () => {
  const broken = '/%7B%22apiToken%22%3A%22abc%22%2C%22jackettApiKey%22%3A%22key%22%7D/manifest.json'
  const repaired = configPath.repairConfiguredPath(broken)
  assert.equal(repaired, broken)
})

test('repairConfiguredPath re-encodes config split by decoded slashes', () => {
  const broken = '/%7B%22apiUrl%22%3A%22http:/x/rest/1.0%22%2C%22apiToken%22%3A%22t%22%7D/manifest.json'
  const repaired = configPath.repairConfiguredPath(broken)
  assert.ok(repaired)
  assert.match(repaired, /\/manifest\.json$/)
  const encodedConfig = repaired.slice(1, repaired.indexOf('/manifest.json'))
  const parsed = JSON.parse(decodeURIComponent(encodedConfig))
  assert.equal(parsed.apiUrl, 'http://x/rest/1.0')
  assert.equal(parsed.apiToken, 't')
})
