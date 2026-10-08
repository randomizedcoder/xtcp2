package linkmonitor

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func (c *reconciler) acceptStatistics(records []model.LinkStatistics) error {
	if len(records) > maxInventoryDevices {
		return fmt.Errorf("inventory statistics exceed bound")
	}
	c.statistics = make(map[model.DeviceKey]*model.LinkStatistics, len(records))
	for i := range records {
		record := &records[i]
		if err := validTraffic(record); err != nil {
			return err
		}
		if !record.Observed.Present || record.Observed.Value.Monotonic < 0 {
			return fmt.Errorf("inventory statistics lack observation time")
		}
		if _, exists := c.candidate[record.Key]; !exists {
			return fmt.Errorf("statistics without inventory identity")
		}
		if _, exists := c.statistics[record.Key]; exists {
			return fmt.Errorf("duplicate inventory statistics")
		}
		c.statistics[record.Key] = record
	}
	return nil
}
