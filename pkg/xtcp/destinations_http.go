package xtcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// httpDest POSTs each flushed batch to an HTTP(S) endpoint:
// `-dest http://host:port/path` or `-dest https://...`. Works with generic
// ingest endpoints and log/metric shippers (Vector, Loki push, Elasticsearch
// bulk, Splunk HEC, …). The Content-Type is derived from the marshaller so a
// receiver can route by type. Framing is the marshaller's responsibility.
//
// One POST per flush (per poll cycle / size-cap). Non-2xx responses are
// errors. A keep-alive client is reused across sends.
type httpDest struct {
	x           *XTCP
	url         string
	contentType string
	client      *http.Client
	timeout     time.Duration
}

const httpDestDefaultTimeout = 10 * time.Second

// contentTypeForMarshaller maps a marshaller name to the MIME type a receiver
// would expect, so HTTP consumers can route/parse by Content-Type.
func contentTypeForMarshaller(marshalTo string) string {
	switch marshalTo {
	case MarshallerJSONL:
		return "application/x-ndjson"
	case MarshallerProtoJSON:
		return "application/json"
	case MarshallerCSV:
		return "text/csv"
	case MarshallerTSV:
		return "text/tab-separated-values"
	case MarshallerProtoText:
		return "text/plain; charset=utf-8"
	default: // protobufList, msgpack
		return "application/octet-stream"
	}
}

func newHTTPDest(_ context.Context, x *XTCP) (Destination, error) {
	// x.config.Dest is the full URL (scheme included), e.g.
	// "http://127.0.0.1:8080/ingest" — used verbatim.
	url := x.config.Dest
	timeout := x.config.GetKafkaProduceTimeout().AsDuration()
	if timeout <= 0 {
		timeout = httpDestDefaultTimeout
	}
	return &httpDest{
		x:           x,
		url:         url,
		contentType: contentTypeForMarshaller(x.config.MarshalTo),
		client:      &http.Client{Timeout: timeout},
		timeout:     timeout,
	}, nil
}

func (d *httpDest) Send(ctx context.Context, b *[]byte) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.url, bytes.NewReader(*b))
	if err != nil {
		d.x.pC.WithLabelValues("destHTTP", "newRequest", "error").Inc()
		return 0, fmt.Errorf("destHTTP new request: %w", err)
	}
	req.Header.Set("Content-Type", d.contentType)

	resp, err := d.client.Do(req)
	if err != nil {
		d.x.pC.WithLabelValues("destHTTP", "do", "error").Inc()
		if d.x.debugLevel > 100 {
			log.Printf("destHTTP POST %q err:%v", d.url, err)
		}
		return 0, fmt.Errorf("destHTTP POST %q: %w", d.url, err)
	}
	// Drain and close so the keep-alive connection can be reused.
	if _, derr := io.Copy(io.Discard, resp.Body); derr != nil && d.x.debugLevel > 100 {
		log.Printf("destHTTP drain body err:%v", derr)
	}
	if cerr := resp.Body.Close(); cerr != nil && d.x.debugLevel > 100 {
		log.Printf("destHTTP body close err:%v", cerr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.x.pC.WithLabelValues("destHTTP", "status", "error").Inc()
		return 0, fmt.Errorf("destHTTP POST %q: status %d", d.url, resp.StatusCode)
	}
	d.x.pC.WithLabelValues("destHTTP", "posts", "count").Inc()
	d.x.pC.WithLabelValues("destHTTP", "postBytes", "count").Add(float64(len(*b)))
	return 1, nil
}

func (d *httpDest) Close() error {
	d.client.CloseIdleConnections()
	return nil
}

func init() {
	RegisterDestination(schemeHTTP, newHTTPDest)
	RegisterDestination(schemeHTTPS, newHTTPDest)
}
