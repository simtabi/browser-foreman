# Keep tabs per workspace

Give a workspace its own open-tab snapshots, so its Open tabs and Timeline show only what was open while you worked in it.

## Steps

1. Open *All workspaces…* (the workspace menu in the top bar, or the ⋮ menu).
2. On the workspace's row, open its ⋯ menu and choose *Keep its own tab snapshots*. Its badge reads *own tab snapshots*.
3. Switch to that workspace. From now on, snapshots (automatic or *Save now*) are saved into its own pool while it is
   the workspace you last opened.

Every other workspace keeps sharing one pool, so turning this on for one changes nothing for the rest. To go back,
choose *Use the shared tab snapshots*; its own snapshots are kept.

## Through the API

```bash
curl -X PUT http://127.0.0.1:8765/api/v1/workspaces/work/tabs \
  -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Content-Type: application/json' -d '{"own": true}'
```

How pools work is in [workspaces](../tools/workspaces.md).

---
[← Docs index](../../README.md#documentation)
