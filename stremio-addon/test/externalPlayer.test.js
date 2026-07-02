const test = require('node:test')
const assert = require('node:assert/strict')

const externalPlayer = require('../lib/externalPlayer')

test('play proxy URLs are detected even when the path ends in .mp4', () => {
  const playUrl = externalPlayer.buildPlayUrl(
    'aabbccddeeff001122334455',
    'https://stremio.example.com',
    'Movie.2026.1080p.WEB-DL.mp4',
  )

  assert.match(playUrl, /\.mp4$/)
  assert.equal(externalPlayer.isPlayProxyUrl(playUrl), true)
  assert.equal(externalPlayer.isPlayProxyUrl('https://cdn.example.com/movie.mp4'), false)
})
