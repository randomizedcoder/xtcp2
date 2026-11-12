package goip

// nlmsgAlign is NLMSG_ALIGN: n rounded up to netlink's 4-byte alignment.
func nlmsgAlign(n int) int { return (n + 3) &^ 3 }
