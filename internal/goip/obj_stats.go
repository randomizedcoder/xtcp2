package goip

import (
	"fmt"
)

// runStats is do_ipstats (ip/ipstats.c:1315-1339): a bare `ip stats` is a
// show at the -s level given, `show` adds one level and -d another, `set`
// is statsSet, and `help` the usage on stderr; all by strcmp. Any other
// word is unknown, rc -1, which ip's do_cmd negates to 1.
func runStats(c *runCtx, args []string) error {
	if len(args) == 0 {
		return runStatsShow(c, nil, c.showStats)
	}
	switch args[0] {
	case helpVerbCst:
		_, err := fmt.Fprint(c.errOut, ipstatsHelp())
		return err
	case showVerbCst:
		return runStatsShow(c, args[1:], c.showStats+c.showDetails+1)
	case statsSetVerbCst:
		return writeOps.statsSet(c, args[1:])
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip stats help\"", ErrInvalidArgument, args[0])
}
