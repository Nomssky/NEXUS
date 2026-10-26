import { test, expect } from '@playwright/test';
import { fixtures, reservePortPair, startNexus, type NexusHandle } from '../fixtures/nexus';
import * as fs from 'node:fs';
import * as path from 'node:path';

/**
 * Black-box validation of the durable-store ownership boundary.
 *
 * The store root is single-writer: exactly one live process may own it. This
 * spec proves the guarantee with real compiled processes — a second process is
 * refused while the first runs, and ownership is released on both an orderly
 * SIGTERM exit and a SIGKILL crash (the kernel drops the flock with the
 * process). It also proves the boot path validates the effective directory,
 * resolves a configured symlink once, and keeps the data owner-only.
 *
 * Nothing here talks to a provider or external network.
 */

function mkScratch(prefix: string): string {
  const root = path.join(fixtures.E2E_DIR, '.tmp');
  fs.mkdirSync(root, { recursive: true });
  return fs.mkdtempSync(path.join(root, prefix));
}

function modeOf(p: string): number {
  return fs.statSync(p).mode & 0o777;
}

/** Boot with an effective data dir that differs from the fixture's scratch dir. */
async function expectStartupFailure(effectiveDir: string, pattern: RegExp): Promise<void> {
  const scratch = mkScratch('storage-reject-');
  let failed: NexusHandle | undefined;
  try {
    let message = '';
    try {
      failed = await startNexus({
        dataDir: scratch,
        extraEnv: { NEXUS_DATA_DIR: effectiveDir },
      });
    } catch (err) {
      message = String(err);
    }
    if (failed) {
      await failed.dispose();
      throw new Error(`startup with data dir ${effectiveDir} unexpectedly succeeded`);
    }
    expect(message).toMatch(pattern);
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true });
  }
}

test.describe('storage ownership', () => {
  test('a second live process cannot own the same store', async () => {
    const nexus = await startNexus();
    let contender: NexusHandle | undefined;
    try {
      let message = '';
      try {
        // Distinct ports isolate the store lock as the refusal reason; the
        // contender dies before it can bind anything.
        contender = await startNexus({
          dataDir: nexus.dataDir,
          ports: await reservePortPair(),
        });
      } catch (err) {
        message = String(err);
      }
      if (contender) {
        await contender.dispose();
        throw new Error('a second process unexpectedly acquired the same store');
      }
      expect(message).toContain('already owned by another process');

      // The refused contender must not disturb the owner.
      const ready = await fetch(`${nexus.baseURL}/ready`);
      expect(ready.status).toBe(200);
      expect(fs.existsSync(path.join(nexus.dataDir, '.nexus-store.lock'))).toBe(true);
    } finally {
      await nexus.dispose();
    }
  });

  test('orderly shutdown releases ownership for the next boot', async () => {
    const nexus = await startNexus();
    try {
      await nexus.stop();
      // A fresh process must acquire the directory immediately, without any
      // stale-lock cleanup step.
      await nexus.restart();
      const ready = await fetch(`${nexus.baseURL}/ready`);
      expect(ready.status).toBe(200);
      expect(fs.existsSync(path.join(nexus.dataDir, '.nexus-store.lock'))).toBe(true);
    } finally {
      await nexus.dispose();
    }
  });

  test('SIGKILL crash releases ownership through the kernel', async () => {
    const nexus = await startNexus();
    try {
      await nexus.kill();
      const lockPath = path.join(nexus.dataDir, '.nexus-store.lock');
      // The marker file survives the crash by design; only the OS lock state
      // is authoritative, so its presence must not block recovery.
      expect(fs.existsSync(lockPath)).toBe(true);

      // A brand-new process must acquire the directory even though the stale
      // marker is still on disk.
      await nexus.restart();
      const ready = await fetch(`${nexus.baseURL}/ready`);
      expect(ready.status).toBe(200);
    } finally {
      await nexus.dispose();
    }
  });

  test('unsafe effective data directories fail startup closed', async () => {
    const scratch = mkScratch('storage-unsafe-');
    try {
      await expectStartupFailure('relative/store', /must be absolute/);
      await expectStartupFailure(path.sep, /filesystem root/);
      await expectStartupFailure('/etc/nexus-e2e-must-not-exist', /sensitive system path/);
      await expectStartupFailure(path.join(scratch, 'missing', 'store'), /must exist/);
      await expectStartupFailure(
        `${scratch}${path.sep}..${path.sep}traversal-store`,
        /traversal segment/,
      );
    } finally {
      fs.rmSync(scratch, { recursive: true, force: true });
    }
  });

  test('a configured symlink is resolved to its canonical target', async () => {
    const scratch = mkScratch('storage-symlink-');
    const target = path.join(scratch, 'target');
    fs.mkdirSync(target, { mode: 0o700 });
    const link = path.join(scratch, 'link');
    fs.symlinkSync(target, link, 'dir');

    const nexus = await startNexus({ dataDir: link });
    try {
      expect(fs.realpathSync(link)).toBe(fs.realpathSync(target));
      // Bootstrap records and the lock are written at the resolved target,
      // never through the configured link path.
      expect(fs.existsSync(path.join(target, '.nexus-store.lock'))).toBe(true);
      const durable = fs
        .readdirSync(target, { withFileTypes: true })
        .filter((e) => e.isDirectory())
        .map((e) => e.name);
      expect(durable.length).toBeGreaterThan(0);
    } finally {
      await nexus.dispose();
      fs.rmSync(scratch, { recursive: true, force: true });
    }
  });

  test('the store root and all durable entries are owner-only', async () => {
    const nexus = await startNexus();
    try {
      expect(modeOf(nexus.dataDir)).toBe(0o700);
      expect(modeOf(path.join(nexus.dataDir, '.nexus-store.lock'))).toBe(0o600);

      const walk = (dir: string): void => {
        for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
          const p = path.join(dir, entry.name);
          if (entry.isDirectory()) {
            expect(modeOf(p), `${p} must be 0700`).toBe(0o700);
            walk(p);
          } else if (entry.isFile()) {
            expect(modeOf(p), `${p} must be 0600`).toBe(0o600);
          }
        }
      };
      walk(nexus.dataDir);

      // Bootstrap provisioning created real durable records worth checking.
      expect(fs.readdirSync(nexus.dataDir).length).toBeGreaterThan(1);
    } finally {
      await nexus.dispose();
    }
  });
});
