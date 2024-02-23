package ospfip

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
)

var log = clog.NewWithPlugin("ospfip")

const PLUGIN_NAME = "ospfip"
const DEFAULT_REFRESH = 5
const DEFAULT_TTL = 3600

func init() {
	plugin.Register(PLUGIN_NAME, setup)
}

func setup(c *caddy.Controller) error {
	for c.Next() {
		var ttl uint32 = DEFAULT_TTL
		refresh := DEFAULT_REFRESH * time.Minute

		args := c.RemainingArgs()

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
			case "ttl":
				if c.NextArg() {
					ttlStr := c.Val()
					var err error
					ttli, err := strconv.Atoi(ttlStr)
					if err != nil {
						return plugin.Error(PLUGIN_NAME, c.Errf("Unable to parse ttl: %v", err))
					}
					ttl = uint32(ttli)
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
		of := New(osc, refresh, ttl)
		of.Origins = plugin.OriginsFromArgsOrServerBlock(args, c.ServerBlockKeys)

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
