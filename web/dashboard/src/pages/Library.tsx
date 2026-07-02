import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  deleteTorrent,
  deleteTorrents,
  fetchLibraryItem,
  fetchLibraryItems,
  fetchStats,
  joinUrl,
  patchLibraryItem,
  type LibraryDetail,
  type LibraryFilter,
  type LibraryItem,
  type LibrarySort,
  type TorrentFile,
} from '../api';
import CopyButton from '../components/CopyButton';
import Icon from '../components/Icon';
import StatusBadge from '../components/StatusBadge';
import { TopBarActions, TopBarMeta } from '../components/TopBar';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';
import { basename, formatBytes, formatRelativeTime } from '../lib/format';

type SortDir = 'asc' | 'desc';

const FILTERS: { key: LibraryFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'favorites', label: 'Favorites' },
  { key: 'pinned', label: 'Pinned' },
  { key: 'protected', label: 'Protected' },
  { key: 'unprotected', label: 'Unprotected' },
];

const SORTS: { key: LibrarySort; label: string }[] = [
  { key: 'recent', label: 'Recently completed' },
  { key: 'name', label: 'Name' },
  { key: 'size', label: 'Size' },
  { key: 'added', label: 'Added' },
];

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function webdavPath(hash: string, filePath: string): string {
  const cleanHash = hash.trim().toLowerCase();
  const cleanPath = filePath.replace(/\\/g, '/').replace(/^\/+/, '');
  const encodedPath = cleanPath.split('/').map(encodeURIComponent).join('/');
  return `/${encodeURIComponent(cleanHash)}/${encodedPath}`;
}

function fileSelected(file: TorrentFile): boolean {
  return file.selected === true || file.selected === 1;
}

function fileLinkRows(detail: LibraryDetail): { file: TorrentFile; link?: string }[] {
  let linkIndex = 0;
  return detail.files.map((file) => {
    const selected = fileSelected(file);
    const linksVisible = file.linksVisible !== false;
    const link = selected && linksVisible ? detail.links[linkIndex] : undefined;
    if (selected && linksVisible) linkIndex += 1;
    return { file, link };
  });
}

function StateButton({
  active,
  busy,
  icon,
  label,
  activeLabel,
  onClick,
}: {
  active: boolean;
  busy: boolean;
  icon: string;
  label: string;
  activeLabel: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={active ? 'icon-btn library-state-btn active' : 'icon-btn library-state-btn'}
      title={active ? activeLabel : label}
      aria-label={active ? activeLabel : label}
      aria-pressed={active}
      disabled={busy}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
    >
      <Icon name={icon} size={17} />
    </button>
  );
}

function FileRow({
  hash,
  file,
  link,
  publicUrl,
}: {
  hash: string;
  file: TorrentFile;
  link?: string;
  publicUrl: string;
}) {
  const davPath = webdavPath(hash, file.path);
  const fullWebdav = joinUrl(publicUrl.replace(/\/+$/, ''), `/webdav${davPath}`);
  const selected = fileSelected(file);
  const linksVisible = selected && file.linksVisible !== false;
  const streamable = linksVisible && (file.streamableBytes ?? 0) >= file.bytes && file.bytes > 0;

  return (
    <div className="library-file-row">
      <div className="library-file-info">
        <span className="library-file-name">{basename(file.path)}</span>
        <span className="library-file-meta">
          {formatBytes(file.bytes)} · {selected ? 'selected' : 'skipped'}
          {streamable ? ' · streamable' : ''}
          {selected && !linksVisible ? ' · not playable' : ''} · {file.path}
        </span>
      </div>
      <div className="library-file-actions">
        <CopyButton value={davPath} label="Path" />
        {linksVisible && <CopyButton value={fullWebdav} label="WebDAV" />}
        {linksVisible && (
          <a className="btn btn-secondary btn-sm" href={fullWebdav} target="_blank" rel="noreferrer">
            <Icon name="external-link" size={14} />
            Open
          </a>
        )}
        {link && <CopyButton value={link} label="Host" />}
        {link && (
          <a className="btn btn-secondary btn-sm" href={link} target="_blank" rel="noreferrer">
            <Icon name="external-link" size={14} />
            Host
          </a>
        )}
      </div>
    </div>
  );
}

function LibraryCard({
  item,
  detail,
  publicUrl,
  expanded,
  selected,
  busy,
  loadingDetail,
  detailError,
  onToggle,
  onSelect,
  onToggleState,
  onDelete,
}: {
  item: LibraryItem;
  detail?: LibraryDetail;
  publicUrl: string;
  expanded: boolean;
  selected: boolean;
  busy: boolean;
  loadingDetail: boolean;
  detailError?: string;
  onToggle: () => void;
  onSelect: () => void;
  onToggleState: (patch: Partial<Pick<LibraryItem, 'pinned' | 'favorite' | 'retentionExempt'>>) => void;
  onDelete: () => void;
}) {
  const size = item.size > 0 ? item.size : item.bytes;
  const completedAt = item.ended ?? item.added;
  const webdavBase = publicUrl ? joinUrl(publicUrl.replace(/\/+$/, ''), '/webdav/') : '/webdav/';
  const rows = detail ? fileLinkRows(detail) : [];

  return (
    <article className={item.pinned ? 'card library-card pinned' : 'card library-card'}>
      <div className="library-card-main">
        <input
          className="row-select library-select"
          type="checkbox"
          aria-label={`Select ${item.name}`}
          checked={selected}
          onChange={onSelect}
          onClick={(event) => event.stopPropagation()}
        />
        <button type="button" className="library-card-header" onClick={onToggle}>
          <span className="library-card-title">
            <span className="library-title-line">
              <span className="name-primary">{item.name || 'Unnamed download'}</span>
              {item.pinned && <span className="pill pill-accent">Pinned</span>}
              {item.favorite && <span className="pill pill-warning">Favorite</span>}
              {item.retentionExempt && <span className="pill pill-live">Protected</span>}
            </span>
            <span className="name-meta">
              {formatBytes(size)} · {item.selectedFileCount || item.fileCount} file
              {(item.selectedFileCount || item.fileCount) === 1 ? '' : 's'} · completed{' '}
              {formatRelativeTime(completedAt)}
            </span>
          </span>
          <span className="library-card-aside">
            <StatusBadge torrent={item} />
            <Icon name={expanded ? 'chevron-down' : 'chevron-right'} />
          </span>
        </button>
        <div className="library-card-actions" onClick={(event) => event.stopPropagation()}>
          <StateButton
            active={Boolean(item.pinned)}
            busy={busy}
            icon="pin"
            label="Pin item"
            activeLabel="Unpin item"
            onClick={() => onToggleState({ pinned: !item.pinned })}
          />
          <StateButton
            active={Boolean(item.favorite)}
            busy={busy}
            icon="star"
            label="Favorite item"
            activeLabel="Remove favorite"
            onClick={() => onToggleState({ favorite: !item.favorite })}
          />
          <StateButton
            active={Boolean(item.retentionExempt)}
            busy={busy}
            icon="shield"
            label="Exclude from retention"
            activeLabel="Allow retention cleanup"
            onClick={() => onToggleState({ retentionExempt: !item.retentionExempt })}
          />
          <button
            type="button"
            className="icon-btn icon-btn-danger"
            title="Delete item"
            aria-label={`Delete ${item.name}`}
            disabled={busy}
            onClick={onDelete}
          >
            <Icon name="trash-2" size={18} />
          </button>
        </div>
      </div>

      {expanded && (
        <div className="library-card-body">
          {loadingDetail && <p className="muted hint-text">Loading files...</p>}
          {detailError && <p className="error hint-text">{detailError}</p>}
          {detail && detail.files.length === 0 && (
            <p className="muted hint-text">No files listed for this item.</p>
          )}
          {rows.map(({ file, link }) => (
            <FileRow
              key={String(file.id)}
              hash={item.hash}
              file={file}
              link={link}
              publicUrl={publicUrl}
            />
          ))}
          {detail && detail.files.length > 0 && (
            <p className="hint-text">
              WebDAV base: <code>{webdavBase}</code>. Basic auth username is <code>debridnest</code>;
              password is your API token.
            </p>
          )}
        </div>
      )}
    </article>
  );
}

export default function Library() {
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<LibraryFilter>('all');
  const [sort, setSort] = useState<LibrarySort>('recent');
  const [dir, setDir] = useState<SortDir>('desc');
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busyId, setBusyId] = useState<string | null>(null);
  const [details, setDetails] = useState<Record<string, LibraryDetail>>({});
  const [detailErrors, setDetailErrors] = useState<Record<string, string>>({});
  const [loadingDetailId, setLoadingDetailId] = useState<string | null>(null);

  const loader = useCallback(async () => {
    const [items, stats] = await Promise.all([
      fetchLibraryItems({ q: query, filter, sort, dir, limit: 1000 }),
      fetchStats(),
    ]);
    return { items, publicUrl: stats.publicUrl };
  }, [dir, filter, query, sort]);

  const { data, error, loading, updatedAt, refresh } = usePolling(loader);
  const items = data?.items ?? [];

  useEffect(() => {
    const liveIds = new Set(items.map((item) => item.id));
    setSelected((prev) => {
      const next = new Set([...prev].filter((id) => liveIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
    if (expandedId && !liveIds.has(expandedId)) {
      setExpandedId(null);
    }
  }, [expandedId, items]);

  const selectedVisibleIds = useMemo(
    () => [...selected].filter((id) => items.some((item) => item.id === id)),
    [items, selected],
  );
  const allVisibleSelected = items.length > 0 && items.every((item) => selected.has(item.id));

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleSelectAll() {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) {
        items.forEach((item) => next.delete(item.id));
      } else {
        items.forEach((item) => next.add(item.id));
      }
      return next;
    });
  }

  async function loadDetail(id: string) {
    if (details[id]) return;
    setLoadingDetailId(id);
    setDetailErrors((prev) => ({ ...prev, [id]: '' }));
    try {
      const detail = await fetchLibraryItem(id);
      setDetails((prev) => ({ ...prev, [id]: detail }));
    } catch (err) {
      setDetailErrors((prev) => ({
        ...prev,
        [id]: errorMessage(err, 'Failed to load files'),
      }));
    } finally {
      setLoadingDetailId(null);
    }
  }

  async function toggleExpanded(id: string) {
    setExpandedId((prev) => (prev === id ? null : id));
    if (expandedId !== id) {
      await loadDetail(id);
    }
  }

  async function handleState(id: string, patch: Parameters<typeof patchLibraryItem>[1]) {
    setBusyId(id);
    try {
      const updated = await patchLibraryItem(id, patch);
      setDetails((prev) => ({ ...prev, [id]: updated }));
      await refresh();
      toast('Library item updated');
    } catch (err) {
      toast(errorMessage(err, 'Update failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this library item and its files?')) return;
    setBusyId(id);
    try {
      await deleteTorrent(id);
      setSelected((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      setDetails((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
      await refresh();
      toast('Library item deleted');
    } catch (err) {
      toast(errorMessage(err, 'Delete failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleDeleteSelected() {
    const ids = selectedVisibleIds;
    if (ids.length === 0) return;
    if (!confirm(`Delete ${ids.length} selected library item(s) and their files?`)) return;

    setBusyId('bulk');
    try {
      const result = await deleteTorrents(ids);
      const failed = result.failed ?? [];
      setSelected(new Set(failed));
      setDetails((prev) => {
        const next = { ...prev };
        ids.forEach((id) => {
          if (!failed.includes(id)) delete next[id];
        });
        return next;
      });
      await refresh();
      if (result.deleted > 0) {
        toast(`Deleted ${result.deleted} item${result.deleted === 1 ? '' : 's'}`);
      }
      if (failed.length > 0) {
        toast(`Failed to delete ${failed.length} item${failed.length === 1 ? '' : 's'}`, 'error');
      }
    } catch (err) {
      toast(errorMessage(err, 'Delete failed'), 'error');
    } finally {
      setBusyId(null);
    }
  }

  if (loading && !data) {
    return <p className="muted page-loading">Loading library...</p>;
  }

  return (
    <div className="page library-page">
      <TopBarMeta>
        {items.length} item{items.length === 1 ? '' : 's'}
        {updatedAt ? ` · updated ${formatRelativeTime(updatedAt)}` : ''}
      </TopBarMeta>
      <TopBarActions>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => refresh()}>
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </TopBarActions>

      <div className="library-controls">
        <label className="library-search">
          <Icon name="search" size={16} />
          <input
            className="input"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search names, hashes, file paths"
          />
        </label>
        <select
          className="input library-sort"
          value={sort}
          onChange={(event) => setSort(event.target.value as LibrarySort)}
          aria-label="Sort library"
        >
          {SORTS.map((item) => (
            <option key={item.key} value={item.key}>
              {item.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          onClick={() => setDir((prev) => (prev === 'asc' ? 'desc' : 'asc'))}
        >
          <Icon name="arrow-up-down" size={14} />
          {dir === 'asc' ? 'Ascending' : 'Descending'}
        </button>
      </div>

      <div className="page-toolbar library-toolbar">
        <div className="filter-tabs" role="tablist" aria-label="Library filters">
          {FILTERS.map(({ key, label }) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={filter === key}
              className={filter === key ? 'filter-tab active' : 'filter-tab'}
              onClick={() => setFilter(key)}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="toolbar-actions">
          {items.length > 0 && (
            <button type="button" className="btn btn-secondary btn-sm" onClick={toggleSelectAll}>
              {allVisibleSelected ? 'Clear visible' : 'Select visible'}
            </button>
          )}
          {selectedVisibleIds.length > 0 && (
            <button
              type="button"
              className="btn btn-danger btn-sm"
              disabled={busyId === 'bulk'}
              onClick={handleDeleteSelected}
            >
              Delete selected ({selectedVisibleIds.length})
            </button>
          )}
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {items.length === 0 ? (
        <div className="empty-state card">
          <div className="empty-state-icon">
            <Icon name="library" size={20} />
          </div>
          <p>{query || filter !== 'all' ? 'No library items match.' : 'No completed torrents yet.'}</p>
          <p className="muted">Finished downloads appear here with WebDAV paths, host links, and retention controls.</p>
        </div>
      ) : (
        <div className="library-list">
          {items.map((item) => (
            <LibraryCard
              key={item.id}
              item={item}
              detail={details[item.id]}
              publicUrl={data?.publicUrl ?? ''}
              expanded={expandedId === item.id}
              selected={selected.has(item.id)}
              busy={busyId === item.id}
              loadingDetail={loadingDetailId === item.id}
              detailError={detailErrors[item.id]}
              onToggle={() => void toggleExpanded(item.id)}
              onSelect={() => toggleSelect(item.id)}
              onToggleState={(patch) => void handleState(item.id, patch)}
              onDelete={() => void handleDelete(item.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
