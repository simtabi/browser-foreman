# Running at login and notifications

Browser Foreman saves your open tabs and re-checks your links only while `browser-foreman serve` runs. To keep that
going with no terminal and no page open, install it as a login service. When a scheduled re-check finds links that
stopped working, or dead ones that work again, it shows one desktop notification.

## The login service

```bash
browser-foreman service install
browser-foreman service status
browser-foreman service uninstall
```

`install` starts `browser-foreman serve --no-open` now and at every login, from the binary you ran it with. Nothing
installs it for you.

| System | What it installs | Its output |
|---|---|---|
| macOS | a launchd agent, `~/Library/LaunchAgents/com.simtabi.browser-foreman.plist` | `service.log` in the state folder |
| Linux | a systemd user unit, `~/.config/systemd/user/browser-foreman.service` | `journalctl --user -u browser-foreman` |
| Windows | a logon task named *Browser Foreman* in Task Scheduler | none |

- **It restarts after a crash, not after a stop.** `browser-foreman stop` stops it until the next login, and
  `service install` starts it again.
- **It uses your configuration.** The service starts with almost no environment, so `install` carries over
  `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `BROFO_SERVER_ADDR` and `BROFO_STORAGE_PATH` when they are set. The API token
  stays in `.env`, never in the service's definition.
- **Install again after moving the binary.** The service runs the path it was installed with. A binary from
  `go run` is refused, because it is deleted when that command ends.
- **A server already running keeps its address.** If the same version already runs (in a terminal, say), the service
  leaves it be and takes over at the next login. If another version runs there, the service retries every few seconds
  until it stops (`browser-foreman stop`); on Windows it does not retry.
- `uninstall` stops the service and removes its definition. Your bookmarks, tabs and settings stay.

## Desktop notifications

A scheduled re-check (see `recheck_days` in [Browsers and tabs](browsers-and-tabs.md)) shows one notification when it
finds something new:

- **"9 links stopped working"**, for links it confirmed dead: dead on a second check at least a day after the first,
  as [Link checks](link-checks.md) describes. A link dead only once is not counted yet.
- **"2 links work again"**, for links that were dead and now answer.

A re-check that finds nothing new shows nothing, and a check you start from the page reports there instead. Clicking
the notification opens the page. Notifications carry Browser Foreman's own name and icon, through go-notifykit. On
macOS the first one asks whether to allow them, and *System Settings › Notifications › browser-foreman* changes that
later.

| To turn them off | |
|---|---|
| for good | `"notify": {"desktop": false}` in the configuration file |
| for one run | `BROFO_NOTIFY_DESKTOP=0` (or `NOTIFYKIT_DESKTOP=0` for every program using go-notifykit) |

They are always off under `go test` and in CI.

### The `notify` command

```bash
browser-foreman notify doctor                       # do notifications work here, and if not, why
browser-foreman notify send -title "Hello"          # show one from this process
browser-foreman notify ping -title "Backup done"    # hand one to the running server, which shows it
```

`ping` reaches the server through a local socket only it and you can use, so a script or a scheduled job can report
through Browser Foreman's name and icon.

---
[← Docs index](../../README.md#documentation)
