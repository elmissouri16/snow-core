/** End-of-turn "files changed" card, after DeepSeek Harness's deliverables card: a header with the count and a row per file that opens the canvas. */
import type {Touch} from '../canvas/model';
import {basename} from '../canvas/model';

const FileIcon = () => <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M6 2h8l4 4v16H6z M14 2v5h4"/></svg>;

export function ChangedFiles({files, onOpen}: {files: Touch[]; onOpen: (path: string) => void}) {
  if (!files.length) return null;
  const single = files.length === 1 ? files[0]! : null;
  return <div className="changed-files" data-changed-files="" data-count={files.length}>
    <button type="button" className="changed-files-header" onClick={() => onOpen(files[0]!.path)} title="Open in canvas">
      <span className="changed-files-icon"><FileIcon /></span>
      <span className="changed-files-titles">
        <span className="changed-files-title">{single ? basename(single.path) : `${files.length} files changed`}</span>
        <span className="changed-files-stat">{single ? `${single.tool === 'edit' ? 'Edited' : 'Written'} · ${single.path}` : 'Open in canvas'}</span>
      </span>
    </button>
    {!single && <ul className="changed-files-list">{files.map(file => <li key={file.path}>
      <button type="button" data-changed-file={file.path} onClick={() => onOpen(file.path)} title={file.path}><span className="changed-files-name">{basename(file.path)}</span><span className="changed-files-path">{file.path}</span><span className="changed-files-kind">{file.tool === 'edit' ? 'edited' : 'written'}</span></button>
    </li>)}</ul>}
  </div>;
}
