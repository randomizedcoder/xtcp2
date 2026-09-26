package parse

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("text_cidr", textCIDR{}) }

// utf8BOM is the UTF-8 byte-order mark, stripped from feed bodies that include
// it (kept as raw bytes so this source file itself carries no BOM).
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// textCIDR parses feeds that are one CIDR per line (e.g. Cloudflare's
// ips-v4 / ips-v6). Blank lines and `#` comments are ignored. Validation of
// each prefix happens later in the combine stage.
type textCIDR struct{}

func (textCIDR) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	var out []model.Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := meta.Base(retrievedAt)
		r.Prefix = line
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
