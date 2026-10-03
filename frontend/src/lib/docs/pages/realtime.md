# WebSocket protocol

Two sockets, both JSON text frames with a `type` field (ADR-04). The upgrade needs the session cookie, and its `Origin` must match `QP_BASE_URL`'s host; `coder/websocket` enforces this through `OriginPatterns`.

Answers are **not** sent over the socket; they use `PUT /api/attempts/{id}/answers/{qid}`, which is idempotent and retryable. Teacher commands are REST too (D-21). The socket carries events and a few student signals.

## Student: `GET /ws/attempts/{id}`

| Direction | `type` | Fields | When |
|-----------|--------|--------|------|
| ← server | `state` | full `StudentState` | on connect and after every change to the attempt |
| ← server | `time` | `server_time` | every 15 s |
| ← server | `pong` | `t` (echoed), `server_time` | reply to `ping` |
| ← server | `error` | `code`, `message` | a `start` or `violation` was rejected |
| → client | `ping` | `t` = `Date.now()` | about every 10 s; also keeps the socket alive (the server's read timeout is 45 s) |
| → client | `start` | | during the countdown |
| → client | `violation` | `kind`, `client_ts` | `tab_hidden`, `window_blur`, `page_close`, `fullscreen_exit` |

`StudentState` (abridged):

```json
{
  "type": "state", "server_time": 1791035244679,
  "attempt_id": "…", "state": "in_progress", "session_status": "live",
  "countdown_deadline": null, "quiz_deadline": 1791035844679, "question_deadline": 1791035274679,
  "quiz_remaining_ms": null, "question_remaining_ms": null,
  "index": 0, "total": 8, "answered": [false, false, …], "one_way": true,
  "question": { "id": "…", "type": "SINGLE", "text": "…", "body": { "options": [ … ] }, "marks": 1, "resources": [] },
  "answer": null,
  "violation_policy": "invalidate", "warnings": 0, "allowed_warnings": 1, "blur_grace_ms": 2000
}
```

`question` is present only while `in_progress`; it is hidden while paused (UC-03 7b) and never includes the key. `question_deadline` is already capped at the quiz deadline. While paused, the deadlines are null and the `*_remaining_ms` fields hold the frozen time.

### Clock offset

```
offset = server_time − (t0 + rtt / 2)     // from the pong with the smallest round trip
remaining = deadline − (Date.now() + offset)
```

See `ServerClock` in `frontend/src/lib/clock.ts`. The display may drift by tens of milliseconds; the server validates every save against its own clock plus a 2 s grace.

### Disconnects

When an attempt's **last** socket closes during `in_progress`, the server records `disconnected_at`. A reconnect clears it. If it is still set after `disconnect_grace_sec`, the ticker raises a `disconnected` violation (the heartbeat rule). Clients should reconnect with backoff and treat the next `state` as the truth.

## Teacher: `GET /ws/sessions/{id}`

| Direction | `type` | Fields | When |
|-----------|--------|--------|------|
| ← server | `dashboard` | full `Dashboard` (`session`, `counts` per state, `rows`) | on connect, then at most every 500 ms while anything changes |
| ← server | `alert` | `attempt_id`, `student_name`, `student_number`, `kind`, `action`, `state` | immediately on a violation (AC-06: within 2 s) |
| ← server | `session_ended` | | when the session ends |
| ← server | `time`, `pong` | | as for students |
| → client | `ping` | `t` | about every 10 s |

Each dashboard row has the student's name and number, state, current index, answered count, warnings, violations, extra seconds, quiz deadline (or remaining ms while paused) and `connected`.

## Backpressure

Each socket has a 32-message outbound queue. If it fills, for example on a stalled phone, the server closes that socket instead of blocking the hub, and the client reconnects and receives a fresh full state.
