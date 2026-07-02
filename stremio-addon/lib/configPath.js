const URL_CONFIG_KEYS = new Set(['apiUrl', 'jackettUrl'])

function manifestDefaultValues(manifest = {}) {
  const defaults = {}
  for (const field of manifest.config || []) {
    if (field.default !== undefined && field.default !== null && field.default !== '') {
      defaults[field.key] = String(field.default)
    }
  }
  return defaults
}

function buildInstallConfig(formConfig, manifestDefaults = {}) {
  const config = { ...formConfig }
  for (const [key, defaultValue] of Object.entries(manifestDefaults)) {
    if (config[key] === defaultValue) {
      delete config[key]
    }
  }
  for (const key of URL_CONFIG_KEYS) {
    if (key in config) {
      delete config[key]
    }
  }
  for (const [key, value] of Object.entries(config)) {
    if (value === '' || value === undefined || value === null) {
      delete config[key]
    }
  }
  return config
}

function repairConfiguredPath(pathOnly) {
  const streamIdx = pathOnly.indexOf('/stream/')
  const manifestIdx = pathOnly.indexOf('/manifest.json')
  let suffixStart = -1
  if (streamIdx > 0) {
    suffixStart = streamIdx
  } else if (manifestIdx > 0) {
    suffixStart = manifestIdx
  } else {
    return null
  }

  const configPart = pathOnly.slice(1, suffixStart)
  if (!configPart) {
    return null
  }
  const looksLikeJsonConfig = configPart.startsWith('%7B') || configPart.startsWith('{')
  if (!looksLikeJsonConfig) {
    return null
  }

  const suffix = pathOnly.slice(suffixStart)
  let decoded
  try {
    decoded = decodeURIComponent(configPart)
  } catch {
    return null
  }
  decoded = decoded.replace(/http:\/(?!\/)/g, 'http://')
  decoded = decoded.replace(/https:\/(?!\/)/g, 'https://')
  try {
    JSON.parse(decoded)
  } catch {
    return null
  }
  return `/${encodeURIComponent(decoded)}${suffix}`
}

function repairConfiguredAddonRequest(req) {
  const [pathOnly, query = ''] = req.url.split('?')
  const repaired = repairConfiguredPath(pathOnly)
  if (!repaired) {
    return false
  }
  req.url = query ? `${repaired}?${query}` : repaired
  return true
}

module.exports = {
  URL_CONFIG_KEYS,
  manifestDefaultValues,
  buildInstallConfig,
  repairConfiguredPath,
  repairConfiguredAddonRequest,
}
