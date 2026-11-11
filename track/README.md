# Daily progress log

One file per working day: what was planned, what got built, what was learned, and what comes next. The full plan runs from Nov 1 to Nov 30, 2026.

| Day | Date | Focus | Log |
| --- | --- | --- | --- |
| 0 | Oct 31 | Repository setup and tooling | [2026-10-31](2026-10-31.md) |
| 1 | Nov 1 | CRDT IDs, Lamport clock, operations, inserts | [2026-11-01](2026-11-01.md) |
| 2 | Nov 2 | Concurrent insert ordering, tombstone deletes | [2026-11-02](2026-11-02.md) |
| 3 | Nov 3 | Idempotent re-delivery, pending buffer for early ops | [2026-11-03](2026-11-03.md) |
| 4 | Nov 4 (done Nov 5) | Invariants, property tests, fuzzing | [2026-11-04](2026-11-04.md) |
| 5 | Nov 5 | Flat document order, position API, benchmarks | [2026-11-05](2026-11-05.md) |
| 6 | Nov 6 | WebAssembly bridge, Next.js app, CRDT in the browser | [2026-11-06](2026-11-06.md) |
| 7 | Nov 7 | Editor binding, tab-to-tab sync, IME support | [2026-11-07](2026-11-07.md) |
| 8 | Nov 8 (finished Nov 9) | PostgreSQL data layer, transactional outbox | [2026-11-08](2026-11-08.md) |
| 9 | Nov 9 | Sync service actors, WebSocket gateway | [2026-11-09](2026-11-09.md) |
| 10 | Nov 10 | PostgreSQL persistence, catch-up from lastSeq, guest identity | [2026-11-10](2026-11-10.md) |
| 11 | Nov 11 | Browser editing through the gateway, unacked queue | [2026-11-11](2026-11-11.md) |

## Weekly goals

| Week | Dates | Goal |
| --- | --- | --- |
| 1 | Nov 1 to 7 | CRDT core with property tests; WebAssembly build; editor skeleton |
| 2 | Nov 8 to 14 | PostgreSQL, WebSocket server, split into services over gRPC |
| 3 | Nov 15 to 21 | Redis fan-out, presence, offline editing, failure tests |
| 4 | Nov 22 to 30 | Outbox and Kafka workers, metrics, load test, demo |
