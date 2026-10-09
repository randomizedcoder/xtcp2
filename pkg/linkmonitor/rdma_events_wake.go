package linkmonitor

func resourcesWake(s *rdmaEventSchedule) <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.w.wake
}
