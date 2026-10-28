package main

import (
	"fmt"
	"log"
	"time"

	"github.com/dubi-project/dubi-ex/cron"
	"github.com/dubi-project/dubi-ex/event"
	"github.com/dubi-project/dubi-ex/queue"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

var completed = make(chan struct{}, 1)

func init() {
	dubi.Register("example.tick", dubi.Method{Action: func(ctx *dubi.Context) error {
		return event.PublishWithMeta(ctx.Meta, "clock.ticked", Map{"message": "hello from cron"})
	}})
	dubi.Register("clock", cron.Job{Target: "example.tick", Schedule: "@every 1s"})
	dubi.Register("clock.ticked", event.Event{
		Args: Vars{"message": {Type: "string", Required: true}},
		Action: func(ctx *event.Context) {
			fmt.Printf("event: %v\n", ctx.Args["message"])
			if err := queue.PublishWithMeta(ctx.Meta, "message.process", ctx.Args); err != nil {
				ctx.Error(dubi.ErrorResult(err))
			}
		},
	})
	dubi.Register("message.process", queue.Queue{
		Retry: queue.Retry{10 * time.Millisecond},
		Action: func(ctx *queue.Context) {
			if ctx.Attempts() == 1 {
				fmt.Println("queue: retrying once")
				ctx.Retry()
				return
			}
			fmt.Printf("queue: completed attempt=%d message=%v\n", ctx.Attempts(), ctx.Args["message"])
			ctx.Finish()
			select {
			case completed <- struct{}{}:
			default:
			}
		},
	})
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	if err = dubi.DefaultRuntime().Run(); err != nil {
		return err
	}
	defer func() {
		if endErr := dubi.End(); err == nil {
			err = endErr
		}
	}()
	select {
	case <-completed:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("messaging example timed out")
	}
}
