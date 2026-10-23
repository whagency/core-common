# Cron

A lightweight wrapper around [robfig/cron/v3](https://github.com/robfig/cron) for managing scheduled tasks in Go applications.

## Features

- 🕒 **Simple API**: Easy way to add and manage cron jobs.
- 🧩 **Manager Pattern**: Centralized control over starting and stopping all scheduled tasks.
- 🛡️ **Graceful Shutdown**: Supports waiting for running jobs to finish before stopping.
- 📈 **Built-in Observability**: `AddFunc` creates run-scoped logging context and trace span for each execution.

## Installation

```bash
go get github.com/whagency/core-common/cron
```

## Quick Start

### 1. Initialize and Add Jobs

```go
import (
    "context"
    "fmt"
    "log"
    "github.com/whagency/core-common/cron"
)

mgr := cron.NewManager()

// Add a job that runs every minute
err := mgr.AddFunc("* * * * *", "cleanup.every-minute", func(ctx context.Context) error {
    fmt.Println("Running every minute")
    return nil
})
if err != nil {
    log.Fatal(err)
}

// Start the manager
mgr.Start()
```

### 2. Graceful Shutdown

The `Stop()` method returns a `context.Context` that is done when all currently running jobs have completed.

```go
// Stop the manager and wait for jobs to finish
ctx := mgr.Stop()
<-ctx.Done()
```

## API Reference

### `Manager` Methods

#### `NewManager() *Manager`
Creates a new instance of the cron manager.

#### `AddFunc(spec string, jobName string, cmd func(context.Context) error) error`
Adds a function with explicit cron job name and run-scoped observability context.  
For each run, manager creates `request_id` / `internal_trace_id`, logs start/end/error, and opens a trace span.

#### `Start()`
Starts the cron scheduler in its own goroutine.

#### `Stop() context.Context`
Stops the cron scheduler and returns a context that signals when all running jobs are finished.

## Usage Example

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/whagency/core-common/cron"
)

func main() {
    mgr := cron.NewManager()
    
    mgr.AddFunc("*/5 * * * *", "cleanup.task", func(ctx context.Context) error {
        fmt.Println("Cleanup task executed")
        return nil
    })
    
    mgr.Start()
    
    // Wait for termination signal
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
    <-stop
    
    // Shutdown
    fmt.Println("Shutting down...")
    ctx := mgr.Stop()
    <-ctx.Done()
    fmt.Println("Shutdown complete")
}
```

## License

MIT
