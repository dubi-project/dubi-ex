package event

import (
	. "github.com/dubi-project/dubi-go/base"
)

type (
	Event struct {
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
	}

	Events map[string]Event

	Declare struct {
		Alias    []string `json:"alias"`
		Name     string   `json:"name"`
		Desc     string   `json:"desc"`
		Nullable bool     `json:"-"`
		Args     Vars     `json:"args"`
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
