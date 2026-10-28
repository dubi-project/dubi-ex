package queue

import (
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

type (
	Delay = []time.Duration
	Retry = []time.Duration

	Queue struct {
		Alias    []string `json:"alias"`
		Name     string   `json:"name"`
		Desc     string   `json:"desc"`
		Nullable bool     `json:"-"`
		Args     Vars     `json:"args"`
		Settings Map      `json:"settings"`

		Action  ctxFunc   `json:"-"`
		Actions []ctxFunc `json:"-"`

		Found  ctxFunc `json:"-"`
		Error  ctxFunc `json:"-"`
		Failed ctxFunc `json:"-"`
		Denied ctxFunc `json:"-"`

		Connect string `json:"connect"`
		Thread  int    `json:"thread"`
		Retry   Retry  `json:"retry"`
	}

	Queues map[string]Queue

	Declare struct {
		Alias    []string `json:"alias"`
		Name     string   `json:"name"`
		Desc     string   `json:"desc"`
		Nullable bool     `json:"-"`
		Args     Vars     `json:"args"`
	}

	Request struct {
		Name      string
		Data      []byte
		Attempt   int
		Timestamp time.Time
	}

	Response struct {
		Retry bool
		Delay time.Duration
	}

	Filter struct {
		Name     string  `json:"name"`
		Desc     string  `json:"desc"`
		Serve    ctxFunc `json:"-"`
		Request  ctxFunc `json:"-"`
		Execute  ctxFunc `json:"-"`
		Response ctxFunc `json:"-"`
	}

	Handler struct {
		Name   string  `json:"name"`
		Desc   string  `json:"desc"`
		Found  ctxFunc `json:"-"`
		Error  ctxFunc `json:"-"`
		Failed ctxFunc `json:"-"`
		Denied ctxFunc `json:"-"`
	}
)
