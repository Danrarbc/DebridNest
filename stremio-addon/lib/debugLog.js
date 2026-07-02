const DEBUG_SESSION_ID = process.env.DEBUG_SESSION_ID || 'c5d2f9'
const DEBUG_INGEST_URL = process.env.DEBUG_INGEST_URL || ''

function debugLog(payload) {
  const entry = {
    sessionId: DEBUG_SESSION_ID,
    timestamp: Date.now(),
    ...payload,
  }
  console.log(`[agent-debug] ${JSON.stringify(entry)}`)
  if (DEBUG_INGEST_URL) {
    fetch(DEBUG_INGEST_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(entry),
    }).catch(() => {})
  }
}

module.exports = { debugLog }
