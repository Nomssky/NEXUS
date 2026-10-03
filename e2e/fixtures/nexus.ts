import { spawn, ChildProcess, execFile } from 'node:child_process';
import * as fs from 'node:fs';
import * as net from 'node:net';
import * as path from 'node:path';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

const E2E_DIR = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(E2E_DIR, '..');
const BIN_DIR = path.join(E2E_DIR, '.bin');
const BIN_PATH = path.join(BIN_DIR, 'nexus');
const TMP_ROOT = path.join(E2E_DIR, '.tmp');

const BOOTSTRAP_ACTOR = 'nx:human:bootstrap';
const DEFAULT_BUSINESS = 'default';

export interface NexusHandle {
  /** Base URL of the gateway under test. */
  baseURL: string;
  /** Base URL of the health server (separate port: health.port). */
  healthURL: string;
  /** The bootstrap credential handed to the process at boot. */
  credential: string;
  /** Control-plane API key handed to the process at boot. */
  controlKey: string;
  /** Bootstrap identity id. */
  actorID: string;
  /** Bootstrap business id. */
  businessID: string;
  dataDir: string;
  healthPort: number;
  gatewayPort: number;
  stdout(): string;
  stderr(): string;
  /** Terminate the process with SIGTERM and wait for it to exit. */
  stop(): Promise<void>;
  /** Start a fresh process on the same data dir and ports. */
  restart(): Promise<void>;
  /** Stop the process (if running) and delete its temporary data. */
  dispose(): Promise<void>;
}

function log(msg: string): void {
  process.stdout.write(`[nexus-e2e] ${msg}\n`);
}

let buildPromise: Promise<string> | null = null;

/** Build the real NEXUS binary from the current working tree (once per worker). */
async function ensureBinary(): Promise<string> {
  if (!buildPromise) {
    buildPromise = (async () => {
      fs.mkdirSync(BIN_DIR, { recursive: true });
      const started = Date.now();
      try {
        await execFileAsync('go', ['build', '-o', BIN_PATH, './cmd/nexus'], {
          cwd: REPO_ROOT,
          maxBuffer: 16 * 1024 * 1024,
        });
      } catch (err: any) {
        buildPromise = null;
        throw new Error(
          `failed to build ./cmd/nexus: ${err.stderr || err.stdout || err.message}`,
        );
      }
      log(`built ${BIN_PATH} in ${Date.now() - started}ms`);
      return BIN_PATH;
    })();
  }
  return buildPromise;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Reserve a free consecutive port pair (health, health+1 = gateway default). */
async function reservePortPair(): Promise<{ healthPort: number; gatewayPort: number }> {
  for (let attempt = 0; attempt < 50; attempt++) {
    const ports = await Promise.all([listenOnce(), listenOnce()]);
    const sorted = ports.sort((a, b) => a - b);
    const [a, b] = sorted;
    if (b === a + 1) return { healthPort: a, gatewayPort: b };
    // Not consecutive: keep the lower one only if its successor is free.
    const successorFree = await isFree(a + 1);
    if (successorFree) return { healthPort: a, gatewayPort: a + 1 };
  }
  throw new Error('could not reserve a free health/gateway port pair');
}

function listenOnce(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address() as net.AddressInfo;
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}

function isFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const srv = net.createServer();
    srv.once('error', () => resolve(false));
    srv.listen(port, '127.0.0.1', () => srv.close(() => resolve(true)));
  });
}

interface StartOptions {
  dataDir?: string;
  ports?: { healthPort: number; gatewayPort: number };
  /**
   * Extra environment variables for the child process, merged over the
   * harness defaults. Used for configuration that only exists at boot
   * (PROVIDER_CONTRACTS §12): the harness still owns the credentials.
   */
  extraEnv?: Record<string, string>;
}

/**
 * Start the real compiled NEXUS binary as a child process and wait until the
 * gateway reports ready. Nothing here reaches into Go packages: the only
 * contract used is the process environment plus HTTP.
 */
export async function startNexus(opts: StartOptions = {}): Promise<NexusHandle> {
  const bin = await ensureBinary();

  const dataDir = opts.dataDir ?? path.join(TMP_ROOT, `run-${Date.now()}-${process.pid}-${Math.random().toString(36).slice(2, 8)}`);
  fs.mkdirSync(dataDir, { recursive: true });

  const ports = opts.ports ?? (await reservePortPair());
  const credential = `e2e-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
  const controlKey = `e2e-control-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;

  let child: ChildProcess | null = null;
  let stdout = '';
  let stderr = '';
  const capture = (chunk: Buffer, sink: 'out' | 'err') => {
    const text = chunk.toString();
    if (sink === 'out') stdout = (stdout + text).slice(-200_000);
    else stderr = (stderr + text).slice(-200_000);
  };

  const baseURL = `http://127.0.0.1:${ports.gatewayPort}`;
  const healthURL = `http://127.0.0.1:${ports.healthPort}`;

  const env = {
    ...process.env,
    NEXUS_DATA_DIR: dataDir,
    NEXUS_HEALTH_HOST: '127.0.0.1',
    NEXUS_HEALTH_PORT: String(ports.healthPort),
    NEXUS_BOOTSTRAP_CREDENTIAL: credential,
    NEXUS_BOOTSTRAP_BUSINESS: DEFAULT_BUSINESS,
    NEXUS_CONTROL_API_KEY: controlKey,
    NEXUS_LOG_FORMAT: 'json',
    NEXUS_LOG_LEVEL: 'info',
    NEXUS_ENVIRONMENT: 'development',
    ...(opts.extraEnv ?? {}),
  };

  const diagnostics = () =>
    `--- nexus stdout ---\n${stdout}\n--- nexus stderr ---\n${stderr}`;

  async function waitReady(timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    let last = 'never responded';
    while (Date.now() < deadline) {
      if (child && child.exitCode !== null) {
        throw new Error(
          `NEXUS exited during startup with code ${child.exitCode}.\n${diagnostics()}`,
        );
      }
      try {
        const res = await fetch(`${baseURL}/ready`);
        if (res.status === 200) {
          // GET /ready is 200 + {"status":"ready"} only while the engine is
          // RUNNING, so a 200 here is the real readiness signal.
          return;
        }
        last = `ready -> HTTP ${res.status}`;
      } catch (err) {
        last = String(err);
      }
      await sleep(100);
    }
    throw new Error(`NEXUS gateway not ready within ${timeoutMs}ms (${last}).\n${diagnostics()}`);
  }

  async function spawnAndWait(): Promise<void> {
    child = spawn(bin, [], { env, cwd: REPO_ROOT, stdio: ['ignore', 'pipe', 'pipe'] });
    child.stdout!.on('data', (c) => capture(c, 'out'));
    child.stderr!.on('data', (c) => capture(c, 'err'));
    child.on('error', (err) => {
      stderr = `${stderr}\nspawn error: ${err.message}`;
    });
    const exited = new Promise<void>((resolve) => child!.once('exit', () => resolve()));
    await waitReady(30_000);
    // Keep the exit promise referenced so a later crash is observable.
    void exited;
  }

  await spawnAndWait();
  log(`ready ${baseURL} (health ${healthURL}, data ${dataDir})`);

  const handle: NexusHandle = {
    get baseURL() {
      return baseURL;
    },
    get healthURL() {
      return healthURL;
    },
    credential,
    controlKey,
    actorID: BOOTSTRAP_ACTOR,
    businessID: DEFAULT_BUSINESS,
    dataDir,
    healthPort: ports.healthPort,
    gatewayPort: ports.gatewayPort,
    stdout: () => stdout,
    stderr: () => stderr,
    async stop() {
      if (!child || child.exitCode !== null) {
        child = null;
        return;
      }
      const proc = child;
      const done = new Promise<void>((resolve) => proc.once('exit', () => resolve()));
      proc.kill('SIGTERM');
      const timedOut = await Promise.race([done.then(() => false), sleep(15_000).then(() => true)]);
      if (timedOut) {
        proc.kill('SIGKILL');
        await Promise.race([done, sleep(5_000)]);
      }
      child = null;
      log(`stopped ${baseURL}`);
    },
    async restart() {
      await handle.stop();
      stdout = '';
      stderr = '';
      await spawnAndWait();
      log(`restarted ${baseURL}`);
    },
    async dispose() {
      await handle.stop();
      if (!process.env.E2E_KEEP) {
        try {
          fs.rmSync(dataDir, { recursive: true, force: true });
        } catch {
          /* best effort */
        }
      }
    },
  };

  return handle;
}

export const fixtures = {
  BOOTSTRAP_ACTOR,
  DEFAULT_BUSINESS,
  E2E_DIR,
  REPO_ROOT,
};
