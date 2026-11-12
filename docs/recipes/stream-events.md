# Stream events

Follow every change live, and catch up after a reconnect. In a page Browser Foreman serves (pages from other sites
are refused by the Origin check), `EventSource` sends your session cookie and `Last-Event-ID` for you; the stream needs
a viewer, so sign in first:

```js
const events = new EventSource("/api/v1/events");
events.addEventListener("webhook.created", e => {
  const event = JSON.parse(e.data);
  console.log(event.id, event.type, event.data);
});
events.onerror = () => console.log("reconnecting; missed events are replayed");
```

From a shell:

```bash
curl -N http://127.0.0.1:8765/api/v1/events -H "Authorization: Bearer $BROFO_API_TOKEN" -H 'Last-Event-ID: 41'
```

Every type is listed in [events](../tools/events.md).

---
[← Docs index](../../README.md#documentation)
