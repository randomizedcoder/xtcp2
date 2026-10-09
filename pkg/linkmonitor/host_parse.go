package linkmonitor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var (
	errHostMalformed = errors.New("malformed host statistics")
	errHostLimit     = errors.New("host statistics limit exceeded")
)

type hostField struct {
	protocol, field, key, descriptor string
	selected                         bool
}

// hostParser belongs to one worker. Cache strings are owned; neither numeric
// scratch nor mutable schema slices escape into results or snapshots.
type hostParser struct {
	filter        *regexp.Regexp
	cache, fields []hostField
	values        []model.Number
	seen          map[string]struct{}
}

func (p *hostParser) reset() {
	clear(p.fields)
	p.fields, p.values = p.fields[:0], p.values[:0]
	if p.seen == nil {
		p.seen = make(map[string]struct{})
	}
	clear(p.seen)
}

func hostToken(data *[]byte) []byte {
	*data = bytes.TrimLeft(*data, " \t\r\n")
	i := 0
	for i < len(*data) && (*data)[i] != ' ' && (*data)[i] != '\t' && (*data)[i] != '\r' && (*data)[i] != '\n' {
		i++
	}
	token := (*data)[:i]
	*data = (*data)[i:]
	return token
}

func hostLine(data *[]byte) []byte {
	for len(*data) != 0 {
		line, rest, _ := bytes.Cut(*data, []byte{'\n'})
		*data = rest
		line = bytes.Trim(line, " \t\r")
		if len(line) != 0 {
			return line
		}
	}
	return nil
}

func hostIdentifier(name []byte) bool {
	if len(name) == 0 {
		return false
	}
	for i, c := range name {
		valid := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !valid {
			return false
		}
	}
	return true
}

func hostNumber(token []byte) (model.Number, error) {
	if len(token) > 0 && token[0] == '-' {
		value, err := strconv.ParseInt(string(token), 10, 64)
		return model.Signed(value), err
	}
	if len(token) > 0 && token[0] == '+' {
		token = token[1:]
	}
	value, err := strconv.ParseUint(string(token), 10, 64)
	return model.Unsigned(value), err
}

func (p *hostParser) add(protocol, field, value []byte) error {
	if !hostIdentifier(protocol) || !hostIdentifier(field) {
		return fmt.Errorf("%w: invalid name %q/%q", errHostMalformed, protocol, field)
	}
	if len(p.fields) == maximumSamples {
		return errHostLimit
	}
	entry := p.entry(protocol, field)
	if _, duplicate := p.seen[entry.key]; duplicate {
		return fmt.Errorf("%w: duplicate %s", errHostMalformed, entry.key)
	}
	number, err := hostNumber(value)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errHostMalformed, entry.key, err)
	}
	p.seen[entry.key] = struct{}{}
	p.fields = append(p.fields, entry)
	p.values = append(p.values, number)
	return nil
}

func (p *hostParser) entry(protocol, field []byte) hostField {
	i := len(p.fields)
	if i < len(p.cache) && p.cache[i].protocol == string(protocol) && p.cache[i].field == string(field) {
		return p.cache[i]
	}
	entry := hostField{protocol: string(protocol), field: string(field)}
	entry.key = entry.protocol + "_" + entry.field
	entry.selected = p.filter.MatchString(entry.key)
	if entry.selected {
		entry.descriptor = "netstat_" + entry.key
	}
	return entry
}

func (p *hostParser) paired(ctx context.Context, data []byte) error {
	groups := make(map[string]struct{})
	for header := hostLine(&data); header != nil; header = hostLine(&data) {
		if err := ctx.Err(); err != nil {
			return err
		}
		values := hostLine(&data)
		name, valueName := hostToken(&header), hostToken(&values)
		if len(name) < 2 || name[len(name)-1] != ':' || !bytes.Equal(name, valueName) {
			return fmt.Errorf("%w: mismatched protocol %q/%q", errHostMalformed, name, valueName)
		}
		protocol := name[:len(name)-1]
		if !hostIdentifier(protocol) {
			return fmt.Errorf("%w: invalid protocol %q", errHostMalformed, protocol)
		}
		if _, duplicate := groups[string(protocol)]; duplicate {
			return fmt.Errorf("%w: repeated protocol %q", errHostMalformed, protocol)
		}
		groups[string(protocol)] = struct{}{}
		if err := p.pair(ctx, protocol, header, values); err != nil {
			return err
		}
	}
	return nil
}

func (p *hostParser) pair(ctx context.Context, protocol, header, values []byte) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		field, value := hostToken(&header), hostToken(&values)
		if len(field) == 0 && len(value) == 0 {
			return nil
		}
		if len(field) == 0 || len(value) == 0 {
			return fmt.Errorf("%w: field count for %q", errHostMalformed, protocol)
		}
		if err := p.add(protocol, field, value); err != nil {
			return err
		}
	}
}

func (p *hostParser) ipv6(ctx context.Context, data []byte) error {
	for line := hostLine(&data); line != nil; line = hostLine(&data) {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, value := hostToken(&line), hostToken(&line)
		six := bytes.IndexByte(name, '6')
		if six < 1 || six == len(name)-1 || len(value) == 0 || len(hostToken(&line)) != 0 {
			return fmt.Errorf("%w: IPv6 field %q", errHostMalformed, name)
		}
		if err := p.add(name[:six+1], name[six+1:], value); err != nil {
			return err
		}
	}
	return nil
}

func (p *hostParser) samples() []model.Sample {
	count := 0
	for _, entry := range p.fields {
		if entry.selected {
			count++
		}
	}
	samples := make([]model.Sample, 0, count)
	for i, entry := range p.fields {
		if entry.selected {
			samples = append(samples, model.Sample{Descriptor: entry.descriptor, Kind: model.SampleUntyped, Number: p.values[i]})
		}
	}
	// Clear removed strings from reusable backing arrays, including failed parses.
	clear(p.cache)
	p.cache, p.fields = p.fields, p.cache[:0]
	return samples
}
