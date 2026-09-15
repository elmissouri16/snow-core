import type { LoginProps } from './model';
import { Icon } from './Icons';
export function LoginPage({ csrf, error }: LoginProps) {
  return (
    <main className="login-card">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true">
          <Icon kind="snow" />
        </span>
        <span className="brand-name">snow</span>
        <span className="brand-label">WORKSPACE</span>
      </div>
      <span className="eyebrow">ONE OPERATOR. ONE HOST.</span>
      <h1>Your work, right here.</h1>
      <p className="lede">
        Pair this browser with the pairing code from the terminal where Snow is
        running.
      </p>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <form method="post" action="/login">
        <input type="hidden" name="csrf" value={csrf} />
        <label htmlFor="code">Pairing code</label>
        <input
          id="code"
          name="code"
          type="password"
          required
          maxLength={128}
          autoComplete="off"
          spellCheck={false}
          autoFocus
        />
        <button className="primary" type="submit">
          Connect to this host <span aria-hidden="true">→</span>
        </button>
      </form>
      <p className="fine">
        The pairing code is reusable for up to 30 days, or until rotated. Paired
        browsers stay connected across Snow restarts. Credentials stay out of
        URLs and browser history.
      </p>
      <div className="local-note">
        <span className="status-dot" />
        Direct local connection
      </div>
    </main>
  );
}
