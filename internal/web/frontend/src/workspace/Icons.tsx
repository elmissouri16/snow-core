export function Icon({
  kind,
}: {
  kind: 'snow' | 'folder' | 'chevron' | 'plus' | 'close' | 'panel';
}) {
  return (
    <svg
      className="icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      {kind === 'folder' ? (
        <path d="M3 7h6l2-3h5l2 3h3v13H3z" />
      ) : kind === 'chevron' ? (
        <path d="m8 10 4 4 4-4" />
      ) : kind === 'plus' ? (
        <path d="M12 5v14M5 12h14" />
      ) : kind === 'close' ? (
        <path d="m6 6 12 12M6 18 18 6" />
      ) : kind === 'panel' ? (
        <>
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M9 4v16" />
        </>
      ) : (
        <path d="M12 2v20M3.34 7l17.32 10M3.34 17 20.66 7m-12-3L12 7l3.34-3M4 10l4 1v4l-4 1m4.66 4L12 17l3.34 3M20 10l-4 1v4l4 1" />
      )}
    </svg>
  );
}
