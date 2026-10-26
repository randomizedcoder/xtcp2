// Package local is the host-side configuration shared by every process that
// touches the treatment journal (the embedded or standalone agent, the
// watchdog and the treatmentctl commands) and the construction of the
// kernel-backed reconciler from it. It never speaks gRPC, never reads TLS
// material and never resolves DNS: the watchdog must work with the controller
// unreachable and the certificate expired.
package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// Config is /etc/xtcp2/controller-agent.json. The controller and credential
// fields are only read by the agent; the watchdog ignores them.
type Config struct {
	AgentID           string   `json:"agent_id"`
	Location          string   `json:"location"`
	Controller        string   `json:"controller"`
	ServerName        string   `json:"server_name"`
	Certificate       string   `json:"certificate"`
	PrivateKey        string   `json:"private_key"`
	CA                string   `json:"ca"`
	Directory         string   `json:"directory"`
	EnableApply       bool     `json:"enable_apply"`
	Table             uint32   `json:"table"`
	RulePriority      uint32   `json:"rule_priority"`
	Protocol          uint8    `json:"protocol"`
	ProtectedPrefixes []string `json:"protected_prefixes"`
}

// DefaultDirectory is where the journal lives unless the config says otherwise.
const DefaultDirectory = "/var/lib/xtcp2/routes"

// LoadConfig reads and validates the JSON file, applying the private route
// table defaults. Unknown fields and trailing documents are errors so a typo
// can never silently disable a setting.
func LoadConfig(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("trailing config data")
	}
	if !treatment.ValidID(c.AgentID) || !treatment.ValidID(c.Location) || c.Controller == "" {
		return c, errors.New("agent_id, location and controller are required")
	}
	c.applyDefaults()
	return c, nil
}

func (c *Config) applyDefaults() {
	if c.Directory == "" {
		c.Directory = DefaultDirectory
	}
	if c.Table == 0 {
		c.Table = 10002
	}
	if c.RulePriority == 0 {
		c.RulePriority = 30000
	}
	if c.Protocol == 0 {
		c.Protocol = 242
	}
}

func (c Config) String() string {
	return fmt.Sprintf("agent=%s location=%s controller=%s apply=%t", c.AgentID, c.Location, c.Controller, c.EnableApply)
}
