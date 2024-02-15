package ospfip

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

const PLUGIN_NAME = "ospfip"
const DEFAULT_REFRESH = 5

func init() {
	plugin.Register(PLUGIN_NAME, setup)
}

func setup(c *caddy.Controller) error {
	for c.Next() {
		refresh := time.Duration(DEFAULT_REFRESH) * time.Minute
		for c.NextBlock() {
			switch c.Val() {
			case "refresh":
				if c.NextArg() {
					refreshStr := c.Val()
					_, err := strconv.Atoi(refreshStr)
					if err == nil {
						refreshStr = fmt.Sprintf("%ss", c.Val())
					}
					refresh, err = time.ParseDuration(refreshStr)
					if err != nil {
						return plugin.Error(PLUGIN_NAME, c.Errf("Unable to parse duration: %v", err))
					}
					if refresh <= 0 {
						return plugin.Error(PLUGIN_NAME, c.Errf("refresh interval must be greater than 0: %q", refreshStr))
					}
				} else {
					return plugin.Error(PLUGIN_NAME, c.ArgErr())
				}
			default:
				return plugin.Error(PLUGIN_NAME, c.Errf("unknown property %q", c.Val()))
			}
		}
		osc, err := NewOpenStackClient()
		if err != nil {
			return plugin.Error(PLUGIN_NAME, c.Errf("failed to initialize %s plugin: %v", PLUGIN_NAME, err))
		}
		ctx, cancel := context.WithCancel(context.Background())
		of := New(osc, refresh)

		if err := of.Run(ctx); err != nil {
			cancel()
			return plugin.Error(PLUGIN_NAME, c.Errf("failed to initialize %s plugin: %v", PLUGIN_NAME, err))
		}
		dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
			of.Next = next
			return of
		})

		c.OnShutdown(func() error { cancel(); return nil })
	}

	return nil
}
