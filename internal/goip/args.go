package goip

import "fmt"

// ifNameSizeCst is IFNAMSIZ from include/uapi/linux/if.h, NUL included.
const ifNameSizeCst = 16

// checkIfname is lib/utils.c's check_ifname, which mirrors the kernel's
// dev_valid_name.
func checkIfname(name string) bool {
	if name == "" || len(name) >= ifNameSizeCst || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' || cIsSpace(name[i]) {
			return false
		}
	}
	return true
}

// The error text iplink_parse prints, from lib/utils.c.
func invargErr(msg, arg string) error {
	return fmt.Errorf("%w: Error: argument %q is wrong: %s", ErrInvalidArgument, arg, msg)
}

func dupargErr(key, arg string) error {
	return fmt.Errorf("%w: Error: duplicate %q: %q is the second value", ErrInvalidArgument, key, arg)
}

// linkUsage marks err as one of ErrLinkUsage's refusals.
func linkUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrLinkUsage, err)
}

func incompleteErr() error {
	return fmt.Errorf("%w: Command line is not complete. Try option \"help\"", ErrInvalidArgument)
}

func nodevErr(dev string) error {
	return fmt.Errorf("%w: Cannot find device %q", ErrInvalidArgument, dev)
}

// nodevExitErr is a kind's exit(nodev(dev)): nodev's -1 as the status, 255.
func nodevExitErr(dev string) error {
	return linkUsage(nodevErr(dev))
}

// The error text iproute_modify's helpers print, from lib/utils.c.
func duparg2Err(key, arg string) error {
	return fmt.Errorf("%w: Error: either %q is duplicate, or %q is garbage", ErrInvalidArgument, key, arg)
}

func routeHelpErr() error {
	return fmt.Errorf("%w: ip route help is not implemented by goip", ErrNotImplemented)
}

// nextArg is NEXT_ARG: the index of the value, or incomplete_command.
func nextArg(args []string, i int) (int, error) {
	if i+1 >= len(args) {
		return i, incompleteErr()
	}
	return i + 1, nil
}
