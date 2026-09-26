package parse

// opts provides typed access to a source's parser_opts map. YAML decodes
// scalars as int/bool/string, which these helpers coerce with defaults.
type opts map[string]any

func (o opts) str(key, def string) string {
	if v, ok := o[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func (o opts) int(key string, def int) int {
	if v, ok := o[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return def
}

func (o opts) boolean(key string, def bool) bool {
	if v, ok := o[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}
