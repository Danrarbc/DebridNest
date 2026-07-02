const PRESETS = [
  {
    id: 'default',
    label: 'Default',
    nameSuffix: '',
    description: '',
    config: {},
  },
  {
    id: 'balanced',
    label: 'Balanced',
    nameSuffix: 'Balanced',
    description: 'Balanced quality filters for most Stremio clients.',
    config: {
      maxResolution: '1080',
      maxFileSizeGb: '25',
      preferSdr: 'false',
      qualityFilter: '1080p,720p',
      sourceFilter: 'Remux,BluRay,WEB-DL,WEBRip',
    },
  },
  {
    id: '1080p-sdr',
    label: '1080p SDR',
    nameSuffix: '1080p SDR',
    description: '1080p-first SDR-friendly streams for clients with HDR playback issues.',
    config: {
      maxResolution: '1080',
      maxFileSizeGb: '20',
      preferSdr: 'true',
      qualityFilter: '1080p,720p',
      sourceFilter: 'BluRay,WEB-DL,WEBRip,HDTV',
    },
  },
  {
    id: '4k',
    label: '4K',
    nameSuffix: '4K',
    description: 'Prefer high-resolution streams while keeping all source types available.',
    config: {
      maxResolution: '2160',
      maxFileSizeGb: '0',
      preferSdr: 'false',
      qualityFilter: '4K,1080p',
      sourceFilter: 'Remux,BluRay,WEB-DL,WEBRip',
    },
  },
  {
    id: 'low-bandwidth',
    label: 'Low bandwidth',
    nameSuffix: 'Low bandwidth',
    description: 'Smaller files for remote or bandwidth-constrained playback.',
    config: {
      maxResolution: '720',
      maxFileSizeGb: '8',
      preferSdr: 'true',
      qualityFilter: '720p,480p,SD',
      sourceFilter: 'WEB-DL,WEBRip,HDTV',
    },
  },
]

const PRESETS_BY_ID = new Map(PRESETS.map((preset) => [preset.id, preset]))
const PRESETS_BY_NORMALIZED_LABEL = new Map(
  PRESETS.map((preset) => [normalizePresetKey(preset.label), preset]),
)

function normalizePresetKey(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/&/g, ' and ')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function resolvePreset(value) {
  const key = normalizePresetKey(value)
  if (!key || key === 'none' || key === 'auto') {
    return PRESETS_BY_ID.get('default')
  }
  return PRESETS_BY_ID.get(key)
    || PRESETS_BY_NORMALIZED_LABEL.get(key)
    || PRESETS_BY_ID.get('default')
}

function presetOptions() {
  return PRESETS.map((preset) => preset.label)
}

function presetDefaultsByLabel() {
  return Object.fromEntries(PRESETS.map((preset) => [preset.label, preset.config]))
}

function customManifestLabel(userConfig = {}) {
  const raw = userConfig.manifestLabel
    || userConfig.addonLabel
    || ''
  const label = String(raw).trim()
  if (!label) {
    return null
  }
  const id = normalizePresetKey(label)
  if (!id) {
    return null
  }
  return { id, label }
}

function clone(value) {
  return JSON.parse(JSON.stringify(value))
}

function removeConfigurationHints(manifest) {
  manifest.behaviorHints = {
    ...(manifest.behaviorHints || {}),
    configurable: false,
    configurationRequired: false,
  }
  delete manifest.config
  return manifest
}

function buildConfiguredManifest(baseManifest, userConfig = {}) {
  const manifest = removeConfigurationHints(clone(baseManifest))
  const preset = resolvePreset(userConfig.preset || userConfig.manifestPreset)
  const customLabel = customManifestLabel(userConfig)
  const suffix = customLabel || (
    preset.id === 'default'
      ? { id: preset.id, label: preset.nameSuffix || preset.label }
      : { id: preset.id, label: preset.nameSuffix || preset.label }
  )

  if (suffix && suffix.id !== 'default') {
    manifest.id = `${manifest.id}.${suffix.id}`
    manifest.name = `DebridNest: ${suffix.label}`
  } else {
    manifest.name = 'DebridNest'
  }

  if (preset.description) {
    manifest.description = `${manifest.description} ${preset.description}`.trim()
  }

  return manifest
}

function configuredDefaults(userConfig = {}) {
  const preset = resolvePreset(userConfig.preset || userConfig.manifestPreset)
  return { ...preset.config }
}

module.exports = {
  PRESETS,
  resolvePreset,
  presetOptions,
  presetDefaultsByLabel,
  buildConfiguredManifest,
  configuredDefaults,
  normalizePresetKey,
}
