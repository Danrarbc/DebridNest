const test = require('node:test')
const assert = require('node:assert/strict')

const externalPlayer = require('../lib/externalPlayer')
const streamHints = require('../lib/streamHints')

test('play proxy streams omit notWebReady by default for Stremio 5 web UI', () => {
  const playUrl = externalPlayer.buildPlayUrl(
    'aabbccddeeff001122334455',
    'https://stremio.example.com',
    'Movie.2026.1080p.WEBRip.mkv',
  )

  assert.equal(streamHints.shouldMarkNotWebReady(playUrl, 'Movie.2026.1080p.WEBRip.mkv'), false)

  const hints = streamHints.buildStreamBehaviorHints(
    { torrent: { title: 'Movie' } },
    {},
    playUrl,
    'Movie.2026.1080p.WEBRip.mkv',
  )
  assert.equal(hints.notWebReady, undefined)
})

test('forceNotWebReady restores legacy desktop hint behavior', () => {
  const playUrl = externalPlayer.buildPlayUrl(
    'aabbccddeeff001122334455',
    'https://stremio.example.com',
    'Movie.2026.1080p.WEBRip.mkv',
  )

  assert.equal(
    streamHints.shouldMarkNotWebReady(playUrl, 'Movie.2026.1080p.WEBRip.mkv', { forceNotWebReady: true }),
    true,
  )
})
