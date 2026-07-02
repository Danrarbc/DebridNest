const externalPlayer = require('./externalPlayer')
const rank = require('./rank')

function isDirectMp4Https(url, filename = '') {
  const value = String(url || '')
  if (!value.startsWith('https://')) {
    return false
  }
  const name = filename || value.split('/').pop() || ''
  try {
    return /\.mp4(?:[?#]|$)/i.test(decodeURIComponent(name))
  } catch {
    return /\.mp4(?:[?#]|$)/i.test(name)
  }
}

/**
 * Stremio 5 desktop loads web.stremio.com and hides every stream with notWebReady.
 * Omit the flag by default so streams appear; opt in via forceNotWebReady for legacy clients.
 */
function shouldMarkNotWebReady(streamUrl, streamFilename, { forceNotWebReady = false } = {}) {
  if (forceNotWebReady) {
    return externalPlayer.isPlayProxyUrl(streamUrl) || !isDirectMp4Https(streamUrl, streamFilename)
  }
  return false
}

function buildStreamBehaviorHints(entry, streamOptions, streamUrl, streamFilename, hintOptions = {}) {
  const hints = {
    bingeGroup: rank.formatBingeGroup(entry, streamOptions),
    filename: streamFilename,
  }
  if (shouldMarkNotWebReady(streamUrl, streamFilename, hintOptions)) {
    hints.notWebReady = true
  }
  return hints
}

module.exports = {
  isDirectMp4Https,
  shouldMarkNotWebReady,
  buildStreamBehaviorHints,
}
