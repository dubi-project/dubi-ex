package mutex

import (
	"time"

	base "github.com/dubi-project/dubi-go/base"
)

type (
	DebugInfo struct {
		Opened          bool
		Started         bool
		CleanupInterval time.Duration
		Instances       map[string]DebugInstance
		Tokens          []DebugToken
		Stats           Statistics
	}

	DebugInstance struct {
		Name            string
		Type            string
		Weight          int
		Prefix          string
		Expire          time.Duration
		CleanupInterval time.Duration
		TokenGrace      time.Duration
		Settings        base.Map
		Capability      Capability
		Stats           Statistics
	}

	DebugToken struct {
		Conn    string
		Key     string
		Count   int
		Active  int
		Expired int
		Entries []DebugTokenEntry
	}

	DebugTokenEntry struct {
		Active         bool
		Remaining      time.Duration
		GraceRemaining time.Duration
	}
)

func capabilityOf(conn Connection) Capability {
	if conn == nil {
		return Capability{}
	}
	if provider, ok := conn.(CapabilityProvider); ok {
		return provider.Capabilities()
	}
	_, checker := conn.(Checker)
	_, refresher := conn.(Refresher)
	_, token := conn.(TokenConnection)
	_, tokenRefresher := conn.(TokenRefresher)
	return Capability{
		Check:        checker,
		Refresh:      refresher || tokenRefresher,
		Token:        token,
		TokenRefresh: tokenRefresher,
	}
}
