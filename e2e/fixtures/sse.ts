import * as http from 'node:http';

/**
 * Minimal SSE client for black-box tests.
 *
 * Playwright's APIRequestContext buffers a response until it ends, which would
 * hang for the lifetime of a server-sent event stream, so the SSE assertions
 * use node:http directly. Only what the black-box contract needs is modelled:
 * status, headers, parsed frames, and a bounded wait for a matching frame.
 */

export interface SSEFrame {
  /** The `event:` field (may be empty when the server omits it). */
  event: string;
  /** The `data:` payload, joined across multi-line data fields. */
  data: string;
  /** `data` parsed as JSON, when it is valid JSON. */
  json?: Record<string, unknown>;
}

export interface SSEConnection {
  /** HTTP status of the stream response (0 until the response arrived). */
  statusCode: number;
  /** Response headers of the stream response. */
  headers: http.IncomingHttpHeaders;
  /** True once the server (or the socket) ended the stream. */
  ended: boolean;
  /** Every frame received so far, in order. */
  frames: SSEFrame[];
  /** Raw text received so far — for failure messages. */
  raw: string;
  /** Resolves when the socket is fully closed. */
  closed: Promise<void>;
  /** Wait for a frame matching `predicate`, or fail with a diagnostic. */
  waitForFrame(
    predicate: (frame: SSEFrame, index: number) => boolean,
    timeoutMs: number,
    description: string,
  ): Promise<SSEFrame>;
  /** Destroy the client side of the stream. */
  close(): void;
}

function parseFrame(raw: string): SSEFrame | null {
  let event = '';
  const dataLines: string[] = [];
  let sawField = false;

  for (const line of raw.split('\n')) {
    if (line === '' || line.startsWith(':')) continue; // blank or comment
    const idx = line.indexOf(':');
    const field = idx === -1 ? line : line.slice(0, idx);
    let value = idx === -1 ? '' : line.slice(idx + 1);
    if (value.startsWith(' ')) value = value.slice(1);

    if (field === 'event') {
      event = value;
      sawField = true;
    } else if (field === 'data') {
      dataLines.push(value);
      sawField = true;
    } else if (field === 'id' || field === 'retry') {
      sawField = true;
    }
  }

  if (!sawField) return null;
  const data = dataLines.join('\n');
  const frame: SSEFrame = { event, data };
  if (data.length > 0) {
    try {
      const parsed = JSON.parse(data);
      if (parsed !== null && typeof parsed === 'object') {
        frame.json = parsed as Record<string, unknown>;
      }
    } catch {
      // Non-JSON data is still a valid SSE frame; leave json undefined.
    }
  }
  return frame;
}

interface Waiter {
  predicate: (frame: SSEFrame, index: number) => boolean;
  description: string;
  resolve: (frame: SSEFrame) => void;
  reject: (err: Error) => void;
  timer: NodeJS.Timeout;
}

/**
 * Open an SSE stream. Returns immediately with status 0; use
 * `waitForFrame` (or poll `statusCode`) to observe the response.
 */
export function openSSE(
  baseURL: string,
  path: string,
  headers: Record<string, string>,
): SSEConnection {
  const url = new URL(path, baseURL);
  const waiters: Waiter[] = [];
  let settleClosed: () => void;
  const closed = new Promise<void>((resolve) => {
    settleClosed = resolve;
  });

  const conn: SSEConnection = {
    statusCode: 0,
    headers: {},
    ended: false,
    frames: [],
    raw: '',
    closed,
    waitForFrame(predicate, timeoutMs, description) {
      // A frame already satisfying the predicate wins immediately.
      for (let i = 0; i < conn.frames.length; i++) {
        if (predicate(conn.frames[i], i)) return Promise.resolve(conn.frames[i]);
      }
      if (conn.ended) {
        return Promise.reject(
          new Error(
            `${description}: stream already ended after ${conn.frames.length} frame(s)\n` +
              diagnostics(),
          ),
        );
      }
      return new Promise<SSEFrame>((resolve, reject) => {
        const waiter: Waiter = {
          predicate,
          description,
          resolve,
          reject,
          timer: setTimeout(() => {
            const at = waiters.indexOf(waiter);
            if (at >= 0) waiters.splice(at, 1);
            reject(new Error(`${description}: timed out after ${timeoutMs}ms\n${diagnostics()}`));
          }, timeoutMs),
        };
        waiters.push(waiter);
      });
    },
    close() {
      request.destroy();
      settleClosed();
    },
  };

  function diagnostics(): string {
    return [
      `status: ${conn.statusCode}`,
      `frames: ${conn.frames.length}`,
      `ended: ${conn.ended}`,
      'received (truncated to 4000 chars):',
      conn.raw.slice(0, 4000),
    ].join('\n');
  }

  function failWaiters(err: Error) {
    while (waiters.length > 0) {
      const w = waiters.shift()!;
      clearTimeout(w.timer);
      w.reject(new Error(`${w.description}: ${err.message}\n${diagnostics()}`));
    }
  }

  const request = http.get(
    {
      host: url.hostname,
      port: url.port,
      path: `${url.pathname}${url.search}`,
      headers: { Accept: 'text/event-stream', ...headers },
    },
    (res) => {
      conn.statusCode = res.statusCode ?? 0;
      conn.headers = res.headers;

      if (conn.statusCode !== 200) {
        // Error responses are complete JSON bodies, not streams.
        res.setEncoding('utf8');
        res.on('data', (chunk: string) => {
          conn.raw += chunk;
        });
        res.on('end', () => {
          conn.ended = true;
          settleClosed();
          failWaiters(new Error(`non-stream response (HTTP ${conn.statusCode})`));
        });
        res.on('error', (err) => {
          conn.ended = true;
          settleClosed();
          failWaiters(err);
        });
        return;
      }

      let buffer = '';
      res.setEncoding('utf8');
      res.on('data', (chunk: string) => {
        conn.raw += chunk;
        buffer += chunk;
        let boundary = buffer.indexOf('\n\n');
        while (boundary !== -1) {
          const rawFrame = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          const frame = parseFrame(rawFrame);
          if (frame) {
            conn.frames.push(frame);
            const index = conn.frames.length - 1;
            for (let i = 0; i < waiters.length; i++) {
              if (waiters[i].predicate(frame, index)) {
                const w = waiters.splice(i, 1)[0];
                clearTimeout(w.timer);
                w.resolve(frame);
                i--;
              }
            }
          }
          boundary = buffer.indexOf('\n\n');
        }
      });
      res.on('end', () => {
        conn.ended = true;
        settleClosed();
        failWaiters(new Error('stream ended'));
      });
      res.on('error', (err) => {
        conn.ended = true;
        settleClosed();
        failWaiters(err);
      });
    },
  );

  request.on('error', (err) => {
    conn.ended = true;
    settleClosed();
    failWaiters(err);
  });

  return conn;
}
