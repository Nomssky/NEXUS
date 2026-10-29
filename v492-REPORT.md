# NEXUS v49.2 — Penpot MCP Design Connection Report

## A. Current Penpot, MCP server, plugin, and OpenCode state

| Component | State | Details |
|-----------|-------|---------|
| Penpot application | **RUNNING** | `http://localhost:9001` (v2.18.2), `/readyz` returns 200 |
| MCP server | **RUNNING** | `http://localhost:4401/mcp` (Streamable HTTP), WebSocket on `ws://localhost:4402` |
| MCP server process | PID `2625038`, `node dist/index.js` (single-user mode) | Built from `/home/pc/penpot/mcp/packages/server` |
| Plugin server | **RUNNING** | `http://localhost:4400/` (vite preview), manifest at `/manifest.json` |
| Plugin server process | PID `2629320` | Built from `/home/pc/penpot/mcp/packages/plugin` |
| Penpot MCP plugin | **NOT CONNECTED** | Built but not loaded in Penpot UI; no active session |
| OpenCode MCP config | **UPDATED** | `penpot` server URL changed to `http://localhost:4401/mcp` |
| Penpot tools | **AVAILABLE** | `execute_code`, `high_level_overview`, `penpot_api_info`, `export_shape`, `import_image` |

## B. Exact source/install paths and version mismatch

- **Source:** `/home/pc/penpot/mcp/` (monorepo with `packages/server`, `packages/plugin`, `packages/common`)
- **Built artifacts:** `/home/pc/penpot/mcp/packages/server/dist/index.js`, `/home/pc/penpot/mcp/packages/plugin/dist/`
- **MCP server version:** `1.0.0` (from package.json)
- **Plugin version:** `1.0.0`
- **Penpot version:** `2.18.2` (from Penpot app CSS/JS version string)
- **MCP protocol version:** `2024-11-05` (returned by `initialize`)
- **No version mismatch** — all components built from the same `/home/pc/penpot/mcp` source tree. `/opt/penpot/mcp` is empty (not a separate installation).

## C. Build and startup results

**MCP server build:** `pnpm run build` succeeded → `dist/index.js` (852 KB)

**MCP server startup:** Started with `node dist/index.js` (single-user mode). Log confirms:
- Tool registration: `execute_code`, `high_level_overview`, `penpot_api_info`, `export_shape`, `import_image`
- WebSocket server on port `4402`
- Streamable HTTP endpoint: `http://localhost:4401/mcp`
- Multi-user mode: false (no user token required)
- Remote mode: false

**Verified endpoints:**
- `POST http://localhost:4401/mcp` with `initialize` → returns serverInfo `{name: "penpot", version: "1.0.0"}` and capabilities
- `POST http://localhost:4401/mcp` with `tools/list` → returns 5 Penpot tools
- `http://localhost:4400/manifest.json` → HTTP 200 (plugin manifest)

**Plugin build:** `pnpm run build` succeeded → `dist/manifest.json`, `dist/index.html`, `dist/plugin.js`, etc.

## D. Plugin initialization and live-session evidence

**Plugin server:** Running on port 4400, serving the manifest at `/manifest.json` (HTTP 200).

**Plugin-Penpot connection:** NOT ESTABLISHED. The Penpot MCP plugin must be loaded into the Penpot UI at `http://localhost:9001` and connected to the MCP server WebSocket at `ws://localhost:4402`. Evidence:

```
Tool execution failed: Error: No Penpot sessions are connected.
Please connect the Penpot MCP plugin and retry.
```

This error was returned by `execute_code`, confirming the server is reachable but no plugin session exists.

**Not a running server or open Penpot page = proof of connection.** The MCP server and Penpot are both running independently; the missing piece is the plugin bridge between them.

## E. OpenCode configuration and actual tool names exposed

**Config file:** `/home/pc/.config/opencode/opencode.json`

**MCP servers section (penpot entry updated):**
```json
"penpot": {
  "type": "remote",
  "url": "http://localhost:4401/mcp"
}
```
(The previous value pointed to `http://localhost:9001/mcp/stream` with an embedded user token; replaced with the local MCP server URL, no token needed in single-user mode.)

**Other MCP servers preserved:** `vercel` (`https://mcp.vercel.com`), `supabase` (`https://mcp.supabase.com/mcp`).

**Actual tool names exposed by the server:**
1. `execute_code` — Executes JavaScript in the Penpot plugin context (requires active session)
2. `high_level_overview` — Returns usage instructions for Penpot tools ✅ verified working
3. `penpot_api_info` — Returns Penpot API type documentation ✅ verified working
4. `export_shape` — Exports a shape as PNG/SVG (requires active session)
5. `import_image` — Imports an image into Penpot (requires active session)

**Verification:** `high_level_overview` and `penpot_api_info` were called successfully through `execute`. `execute_code` and `export_shape` require an active Penpot session (plugin connected).

## F. Proof that the existing NEXUS document and Office frame can be read directly from Penpot

**Not yet proven.** Direct document access requires an active Penpot plugin session, which does not exist yet. The following Penpot API capabilities are available once the session is connected:

- `penpotUtils.getPages()` — enumerate all pages
- `penpotUtils.getPageByName(name)` / `getPageById(id)` — locate pages
- `penpotUtils.findShapes(predicate, root)` / `findShape()` — locate frames by name/type
- `penpotUtils.shapeStructure(shape, maxDepth)` — inspect frame structure, layout, children
- `shape.fills`, `shape.strokes`, `shape.borderRadius`, `fontSize`, `fontFamily`, etc. — read design properties
- `export_shape({ shapeId, format: "svg\|png" })` — visual preview/export

These cannot be exercised until the plugin is connected (see section G).

## G. Remaining user interaction or authentication requirements

**User action required — connect the Penpot MCP plugin:**

1. Open Penpot in your browser: `http://localhost:9001`
2. Open the **existing** NEXUS design document (do not create a new one)
3. Open the Plugins menu → Load plugin → enter development URL: `http://localhost:4400/manifest.json`
4. Open the plugin UI panel
5. Click **"Connect to MCP server"** — status should change from "Not connected" to "Connected"
6. Keep the Penpot tab active and the plugin UI open during the session

**Important notes:**
- Do not create a new Penpot document or overwrite the existing design
- The MCP server runs in single-user mode, so **no authentication token is needed** for the MCP connection itself
- Penpot UI authentication (logging into Penpot) is handled separately by the browser
- The MCP server process (PID 2625038) and plugin server (PID 2629320) must remain running

**After connection:** Re-run `execute_code` to enumerate pages and locate the Office Command Center frame:

```js
const pages = penpotUtils.getPages();  // returns [{id, name}, ...]
```

Then inspect the frame structure:
```js
penpotUtils.shapeStructure(page.root, 3)
```

## H. Every file or configuration changed, including rollback instructions

**Changed:**
- `/home/pc/.config/opencode/opencode.json` — Updated `mcp.servers.penpot.url` from `http://localhost:9001/mcp/stream?userToken=...` to `http://localhost:4401/mcp`

**Built (new artifacts, not modifications to existing source):**
- `/home/pc/penpot/mcp/packages/server/dist/index.js` — MCP server bundle
- `/home/pc/penpot/mcp/packages/server/dist/data/` — copied resources
- `/home/pc/penpot/mcp/packages/server/dist/static/` — copied static files
- `/home/pc/penpot/mcp/packages/plugin/dist/` — plugin build output
- `/home/pc/penpot/mcp/node_modules/` — installed dependencies (pnpm store)
- `/home/pc/.local/node_modules/pnpm/` — pnpm binary (installed locally)

**Rollback instructions:**
- To revert the OpenCode config change: restore `mcp.servers.penpot.url` to its previous value (the `localhost:9001/mcp/stream` URL with token) from the backup at `/home/pc/.config/opencode/opencode.json.bak.1791290558` or `.bak2.1791290692`
- To stop the MCP server: `kill 2625038`
- To stop the plugin server: `kill 2629320`
- No source files were modified; `pnpm install` only added dependencies to the pnpm store

## I. Final status

**`PARTIALLY CONNECTED — USER ACTION REQUIRED`**

The MCP server and plugin server are built, running, and verified. OpenCode is configured to connect to the local MCP server. The Penpot tools are registered and partially functional (`high_level_overview` and `penpot_api_info` work without a session). However, **no Penpot plugin session is connected**, so `execute_code`, `export_shape`, and document inspection cannot yet be performed.

**The user must connect the Penpot MCP plugin** (loaded from `http://localhost:4400/manifest.json`) to the running Penpot instance at `http://localhost:9001`. Once connected, direct design access will be available and the v49 UI can be verified against the actual Penpot Office Command Center frame.

**Do not modify the NEXUS UI until direct design access is verified.**