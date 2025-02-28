module github.com/randomizedcoder/xtcp2

go 1.23

//replace ./pkg/xtcp_config => ./pkg/xtcp_config

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.1-20241127180247-a33202765966.1
	github.com/ClickHouse/ch-go v0.65.0
	github.com/bufbuild/protovalidate-go v0.8.2
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.1
	github.com/nats-io/nats.go v1.37.0
	github.com/nsqio/go-nsq v1.1.0
	github.com/pkg/profile v1.7.0
	github.com/prometheus/client_golang v1.20.5
	github.com/redis/go-redis/v9 v9.6.1
	github.com/twmb/franz-go v1.18.0
	github.com/twmb/franz-go/plugin/kprom v1.1.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
	golang.org/x/sys v0.30.0
	google.golang.org/genproto/googleapis/api v0.0.0-20250204164813-702378808489
	google.golang.org/grpc v1.70.0
	google.golang.org/protobuf v1.36.5
	gopkg.in/fsnotify.v1 v1.4.7
)

require (
	cel.dev/expr v0.19.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/felixge/fgprof v0.9.5 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/cel-go v0.22.1 // indirect
	github.com/google/pprof v0.0.0-20241210010833-40e02aabc2ad // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.61.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.9.0 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/exp v0.0.0-20241217172543-b2144cdd0a67 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250204164813-702378808489 // indirect
	google.golang.org/grpc/cmd/protoc-gen-go-grpc v1.5.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
