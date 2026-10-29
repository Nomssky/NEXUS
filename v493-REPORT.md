# NEXUS v49.3 — Live Penpot MCP Access Verification Report

## 1. MCP Session Verification

### Infrastructure status

| Component | Status | Endpoint |
|-----------|--------|----------|
| Penpot application | RUNNING | `http://localhost:9001` (v2.18.2) |
| MCP server | RUNNING | `http://localhost:4401/mcp` (Streamable HTTP) |
| MCP WebSocket (plugin) | RUNNING | `ws://localhost:4402` |
| Plugin server | RUNNING | `http://localhost:4400/` (manifest at `/manifest.json`) |
| MCP server process | PID `2625038` | `node dist/index.js` (single-user mode) |
| Plugin server process | PID `2629320` | `vite preview --port 4400` |

### Plugin connection

The Penpot MCP plugin WebSocket connected to the MCP server at **18:41:36** (confirmed in server log: `"New WebSocket connection established"`). The user confirmed plugin status shows "Connected" in the Penpot UI.

### Tool verification

| Tool | Status | Evidence |
|------|--------|----------|
| `high_level_overview` | ✅ Working | Called successfully via OpenCode; returns Pentool tool usage instructions |
| `penpot_api_info` | ✅ Working | Called successfully; returns Penpot API type definitions (e.g., `Page`, `File`) |
| `execute_code` | ⚠️ Intermittent | Worked **once** at 18:45:27 (server log: `Task 03b55236 completed: success=true`). Returned `penpotDefined: true` with `penpotKeys: ["ui","utils","closePlugin","on","off","version","root","currentFile","currentPage","selection"]`. In all other calls, `typeof penpot === "undefined"`. |
| `export_shape` | ❌ Fails | Screenshot timeout: `locator.waitFor: Timeout 10000ms exceeded` — waiting for `#screenshot-...` to be visible. Likely because the Penpot browser tab is not the active/visible tab. |
| `import_image` | Not tested | Requires active session; depends on same plugin context as `execute_code` |

The `execute_code` tool's handler (`ExecuteCodeTaskHandler`) injects `penpot`, `storage`, `console`, and `penpotUtils` as function parameters. `console` is consistently available (custom logging object), but `penpot`, `storage`, and `penpotUtils` are `undefined` in most executions. The one successful execution showed `penpot` with keys including `version`, `root`, `currentFile`, `currentPage`, `selection` — proving the Penpot API *is* reachable when the context is properly populated.

## 2. Pages and Frames Discovered

### From user-provided evidence (not direct API access)
The user's screenshot confirms the existing document contains these pages:
- `00 · Cover`
- `01 · Foundations`
- `02 · Screens`

### From direct Penpot API access
**Not obtained.** The `execute_code` tool — which is the only MCP tool capable of enumerating document pages (`penpotUtils.getPages()`) or inspecting frames — could not reliably access the `penpot` global. When it did work (18:45:27), `penpot.currentFile`, `penpot.root`, and `penpot.currentPage` were all available, but no page enumeration code was successfully executed before the context became unavailable again.

**No page IDs, frame IDs, or shape IDs were retrieved from Penpot directly.** All page names above come from the user's screenshot, not from the Penpot API.

## 3. Export Result and Preview

`export_shape` was called with `shapeId: "page"`, `format: "png"`. It **failed**:

```
Error: Error handling task: http error ({:type :server-error, :hint
"locator.waitFor: Timeout 10000ms exceeded.
Call log:
  - waiting for locator('#screenshot-00000000-0000-0000-0000-000000000000') to be visible
    24 × locator resolved to hidden <svg fill=\"none\" width=\"0.01\" height=\"0.01\" ... id=\"screenshot-...\">
", :uri "http://localhost:9001/api/export", :status 500})
```

The export shape operation relies on Penpot's screenshot/export API (`/api/export`), which requires the target element to be visible in the browser. The timeout indicates the Penpot tab may not be active/visible, or the shape locator could not be resolved.

**No preview image was obtained.** No file was saved to disk (no `filePath` was provided).

## 4. Penpot-versus-Frontend Comparison

**Not possible.** Direct inspection of Penpot frames was not achieved, so no concrete comparison between the Penpot design and the running NEXUS frontend could be performed.

- The running NEXUS frontend at `http://localhost:3000` is operational (HTTP 200).
- The v49 Office Command Center implementation is built and served.
- However, without being able to read Penpot frame structure, dimensions, typography, colors, or layout properties, no verified differences can be reported.

Any claim of visual fidelity between the v49 UI and the Penpot design would be based solely on the `02 · Screens.pdf` export (from v49), not on direct Penpot source inspection.

## 5. Version Mismatch

| Component | Version |
|-----------|---------|
| Penpot application | 2.18.2 |
| MCP server plugin version | 2.17.0 |

The Penpot UI displays a version mismatch warning: "server intended for Penpot 2.17.0" while running Penpot 2.18.2. This mismatch is the likely cause of the `penpot` global being `undefined` in `execute_code` — the `ExecuteCodeTaskHandler` captures `penpot` at construction time, and API surface differences between 2.17.0 and 2.18.2 may prevent the `penpot` object from being properly available or stable.

**Does the version mismatch block operations?**
- `high_level_overview`: No (server-side only)
- `penpot_api_info`: No (server-side only)
- `execute_code`: **Yes** — requires `penpot` global from the plugin context, which is `undefined` in most executions
- `export_shape`: **Yes** — requires plugin context + visible Penpot tab; screenshot times out

## 6. Files and Configuration Changed

- `/home/pc/.config/opencode/opencode.json` — Updated `mcp.servers.penpot.url` from `http://localhost:9001/mcp/stream?userToken=...` (with embedded token) to `http://localhost:4401/mcp` (local MCP server, no token needed in single-user mode)
- `/home/pc/penpot/mcp/packages/server/dist/index.js` — Built MCP server bundle (new artifact)
- `/home/pc/penpot/mcp/packages/server/dist/data/` — Copied resources
- `/home/pc/penpot/mcp/packages/server/dist/static/` — Copied static files
- `/home/pc/penpot/mcp/packages/plugin/dist/` — Built plugin output
- `/home/pc/penpot/mcp/node_modules/` — Installed dependencies
- `/home/pc/.local/node_modules/pnpm/` — pnpm binary installed locally

**Rollback:** Restore `opencode.json` from `/home/pc/.config/opencode/opencode.json.bak.1791290558` or `.bak2.1791290692`. Stop servers with `kill 2625038` (MCP) and `kill 2629320` (plugin). No NEXUS source files were modified.

## 7. Repository Safety

All NEXUS repository protections honored:
- `configs/nexus.dev.json`: never read/modified
- `internal/foundation/modelrouter/groq_router_live_test.go`: never touched
- `docs/operations/`, e2e tests 30/31, `node_modules/`, `test-results/`, `workspaces/`: preserved
- Stash `stash@{0}` (`601b233f…`): intact
- All worktrees: preserved
- No credentials printed
- No commits, pushes, merges, or branch switches
- No NEXUS frontend/backend files modified

## 8. Final Verdict

**`PARTIALLY VERIFIED`**

### What is verified
- The Penpot MCP server and plugin are built, running, and connected to the Penpot instance at `localhost:9001`.
- OpenCode is configured to connect to the local MCP server at `http://localhost:4401/mcp`.
- `high_level_overview` and `penpot_api_info` work without a session.
- The `execute_code` tool *can* work — it succeeded once (18:45:27), proving the Penpot API (`penpot.currentFile`, `penpot.root`, `penpot.currentPage`, `penpot.version`) is reachable when the plugin context is properly populated.

### What is not verified
- Direct enumeration of document pages via `penpotUtils.getPages()` — `penpot` was `undefined` in all but one execution.
- Inspection of the Office Command Center frame structure, layout, typography, colors, or components.
- Export of any Penpot frame preview — `export_shape` times out waiting for a visible screenshot target.
- Visual comparison between the Penpot design and the running NEXUS frontend.

### What the user should do next
1. **Keep the Penpot browser tab active and focused** — the `export_shape` screenshot timeout and intermittent `penpot` availability suggest the tab may be suspended by the browser. The overview documentation warns: "keep the Penpot tab active during long MCP sessions."
2. **Re-run `execute_code` to enumerate pages** — once the tab is active, call:
   ```js
   const pages = penpotUtils.getPages(); // [{id, name}, ...]
   ```
   Then locate the Office Command Center frame (likely on the `02 · Screens` page) and inspect it with `penpotUtils.shapeStructure()`.
3. **Address the version mismatch** — the MCP server plugin is built for Penpot 2.17.0 but Penpot is 2.18.2. Rebuilding the plugin against 2.18.2 (or using a matching MCP server release via `npx -y @penpot/mcp@latest`) may stabilize `execute_code` and `export_shape`.

**Do not implement NEXUS UI changes based on the PDF export alone.** Direct Penpot source inspection is required before declaring visual fidelity.