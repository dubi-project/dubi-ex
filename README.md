# dubi-ex
Extension modules for dubi-go.

- [cache](cache/README.md): driver-backed cache with weighted sources and a default in-memory driver.
- [event](event/README.md): publish/subscribe events with filters, handlers, and weighted sources.
- [queue](queue/README.md): worker queues with delayed delivery, retries, and dead letters.
- [cron](cron/README.md): scheduled Method/Service calls with dynamic jobs and execution history.
- [mutex](mutex/README.md): leased locks with ownership tokens, waiting, and automatic renewal.
- [storage](storage/README.md): local file storage, streaming reads, atomic writes, and image processing.

Modules mount themselves on the dubi-go Runtime when imported. Configuration
is loaded once by dubi-go and delivered to each module through `Config`.

Run the standalone memory-cache example:

```sh
go run ./examples/cache
```

Run cron → event → queue together without external services:

```sh
go run ./examples/messaging
```

Only the default in-process drivers are bundled. Module APIs and driver
interfaces follow infrago and use dubi-go's dynamic registration, Meta, and Res.

Run the standalone mutex example:

```sh
go run ./examples/mutex
```

Run the storage example:

```sh
go run ./examples/storage
```
