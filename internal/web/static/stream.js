/* One read-only, instance-bound subscription. Never retries a mutation. */
(() => {
  "use strict";
  const frameLimit = 16 * 1024 * 1024;
  function open({url, signal, snapshot, state, fallback}) {
    let disposed = false, connection, retryTimer, watchdog, failures = 0;
    const clear = () => { clearTimeout(watchdog); watchdog = null; };
    function stop() {
      disposed = true; clearTimeout(retryTimer); clear(); connection?.abort();
      signal?.removeEventListener("abort", stop);
      document.removeEventListener("visibilitychange", visibility);
    }
    function terminal(kind) { if (disposed) return; state(kind); stop(); }
    function alive() { clear(); watchdog = setTimeout(() => connection?.abort(), 25000); }
    async function connect() {
      if (disposed || document.hidden) return;
      const controller = new AbortController(); connection = controller;
      let reader;
      try {
        alive();
        const response = await fetch(url, {method: "GET", credentials: "same-origin", cache: "no-store", headers: {Accept: "text/event-stream"}, signal: controller.signal});
        if (disposed || connection !== controller) return;
        if (response.status === 501) { stop(); fallback(); return; }
        if ([401, 403].includes(response.status)) { terminal("auth_required"); return; }
        if (response.status === 404) { terminal("closed"); return; }
        if (response.status === 409) { terminal("replaced"); return; }
        if (!response.ok || !response.headers.get("content-type")?.startsWith("text/event-stream") || !response.body) throw new Error("Stream unavailable");
        reader = response.body.getReader();
        const decoder = new TextDecoder("utf-8", {fatal: true});
        let buffer = "";
        while (!disposed && connection === controller) {
          const {done, value} = await reader.read();
          if (disposed || controller.signal.aborted || connection !== controller) return;
          if (done) throw new Error("Stream ended");
          alive(); buffer += decoder.decode(value, {stream: true});
          // Server uses LF-delimited bounded frames. Never accumulate an
          // unbounded partial event or accept a silently clipped projection.
          if (buffer.length > frameLimit) throw new Error("Stream frame too large");
          let boundary;
          while ((boundary = buffer.indexOf("\n\n")) !== -1) {
            const frame = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2);
            let event = "message"; const data = [];
            for (const line of frame.split("\n")) {
              if (line.startsWith("event:")) event = line.slice(6).trim();
              else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
            }
            if (["closed", "auth_required", "replaced"].includes(event)) { terminal(event); return; }
            if (event === "snapshot" && data.length) {
              const value = JSON.parse(data.join("\n"));
              if (!value || typeof value !== "object" || !Number.isSafeInteger(value.revision)) throw new Error("Invalid stream snapshot");
              failures = 0; snapshot(value);
              if (disposed || controller.signal.aborted || connection !== controller) return;
            }
          }
        }
      } catch (_) {
        if (!disposed && !document.hidden && connection === controller) {
          state("reconnecting");
          if (!disposed && !document.hidden && connection === controller) {
            retryTimer = setTimeout(connect, Math.min(500 * 2 ** Math.min(failures++, 5), 10000));
          }
        }
      } finally {
        if (connection === controller) clear();
        controller.abort();
        try { await reader?.cancel(); } catch (_) { /* canceled network read */ }
        reader?.releaseLock();
      }
    }
    function visibility() {
      clearTimeout(retryTimer);
      if (document.hidden) { connection?.abort(); clear(); state("paused"); }
      else if (!disposed) connect();
    }
    signal?.addEventListener("abort", stop, {once: true});
    document.addEventListener("visibilitychange", visibility);
    if (signal?.aborted) stop(); else connect();
    return Object.freeze({close: stop});
  }
  window.SnowStream = Object.freeze({open});
})();
