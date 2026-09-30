import { test as base, expect } from '@playwright/test';
import { startNexus, NexusHandle } from './nexus';

/**
 * Worker-scoped real NEXUS process. One gateway serves the whole worker, so a
 * test that pauses the runtime must restore it before it finishes (and the
 * pause/resume spec does).
 */
export const test = base.extend<{ nexus: NexusHandle }>({
  nexus: [
    async ({}, use) => {
      const handle = await startNexus();
      try {
        await use(handle);
      } finally {
        await handle.dispose();
      }
    },
    { scope: 'worker' },
  ],
});

export { expect };
