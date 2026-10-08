package linkmonitor

import (
	"bytes"
	"encoding/hex"
	"regexp"
	"unicode/utf8"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func decodeStatisticSchema(data []byte, count int, include, exclude *regexp.Regexp) (*model.StatisticSchema, error) {
	if count < 0 || count > maximumSamples || len(data) != count*statisticNameBytes {
		return nil, linuxio.ErrReply
	}
	schema := &model.StatisticSchema{Names: make([]string, count), Selected: make([]int, 0, count), Labels: make([][]model.Label, 0, count)}
	seen := make(map[string]struct{}, count)
	for i := range count {
		raw := data[i*statisticNameBytes : (i+1)*statisticNameBytes]
		if end := bytes.IndexByte(raw, 0); end >= 0 {
			raw = raw[:end]
		}
		name := string(raw)
		if _, duplicate := seen[name]; duplicate {
			return nil, linuxio.ErrReply
		}
		seen[name], schema.Names[i] = struct{}{}, name
		if !include.MatchString(name) || exclude.MatchString(name) {
			continue
		}
		encoding := "utf8"
		if !utf8.ValidString(name) {
			name, encoding = hex.EncodeToString(raw), "hex"
		}
		schema.Selected = append(schema.Selected, i)
		schema.Labels = append(schema.Labels, []model.Label{{Name: "statistic", Value: name}, {Name: "encoding", Value: encoding}})
	}
	return schema, nil
}
